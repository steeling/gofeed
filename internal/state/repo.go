package state

import (
	"context"
	"database/sql/driver"
	"time"

	"github.com/golang/glog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var DefaultTimeout = 10 * time.Second

type Status int64

const (
	Unknown Status = iota
	Available
	Complete
	Failed
)

func (e Status) String() string {
	switch e {
	case Available:
		return "Available"
	case Complete:
		return "Complete"
	case Failed:
		return "Failed"
	case Unknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

func (e *Status) Scan(value interface{}) error { *e = Status(value.(int64)); return nil }
func (e Status) Value() (driver.Value, error)  { return int64(e), nil }

type Repo interface {
	Save(ctx context.Context, m Model) bool
	AutoMigrate() error
	GetPotentialLeases(ctx context.Context) ([]*Partition, error)
	GetAvailableItems(ctx context.Context, p *Partition, limit int) ([]*Item, error)
	GetCountByStatus(ctx context.Context, id string) (map[Status]int, error)
	Healthcheck(ctx context.Context) error
	Transaction(ctx context.Context, f func(db *GormRepo) error) error
}

type GormRepo struct {
	*gorm.DB
	Timeout time.Duration
}

func (db *GormRepo) Healthcheck(ctx context.Context) error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func (db *GormRepo) WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if db.Timeout == 0*time.Second {
		db.Timeout = DefaultTimeout
	}
	return context.WithTimeout(ctx, db.Timeout)
}

type Model interface {
	GetID() string
	GetVersion() int
	IncrementVersion()
	DecrementVersion()
}

type BaseModel struct {
	ID        string `gorm:"primaryKey"`
	Version   int    `gorm:"default:0;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (m *BaseModel) GetID() string {
	return m.ID
}

func (m *BaseModel) GetVersion() int {
	return m.Version
}

func (m *BaseModel) IncrementVersion() {
	m.Version++
}

func (m *BaseModel) DecrementVersion() {
	m.Version--
}

func (db *GormRepo) AutoMigrate() error {
	return db.DB.AutoMigrate(&Item{}, &Partition{})
}

func (db *GormRepo) GetPotentialLeases(ctx context.Context) (partitions []*Partition, err error) {
	ctx, cancel := db.WithTimeout(ctx)
	defer cancel()
	return partitions, db.WithContext(ctx).Where(
		"status != ? AND until < ?",
		Complete, time.Now()).Find(&partitions).Error
}

func (db *GormRepo) GetAvailableItems(ctx context.Context, p *Partition, limit int) (items []*Item, err error) {
	ctx, cancel := db.WithTimeout(ctx)
	defer cancel()
	return items, db.WithContext(ctx).Where(
		"partition_id = ? AND status = ? AND gate = ?", p.ID, Available, p.Gate).Limit(limit).Order(
		"updated_at").Find(&items).Error
}

// Save the item. Modified to leverage OCC version control.
// Returns a boolean indicating if the model was successfully saved. If not,
// represents a dirty object.
func (db *GormRepo) Save(ctx context.Context, m Model) bool {
	ctx, cancel := db.WithTimeout(ctx)
	defer cancel()
	version := m.GetVersion()
	m.IncrementVersion()
	err := db.WithContext(ctx).Clauses(clause.Where{
		Exprs: []clause.Expression{clause.Expr{SQL: "version = ?", Vars: []interface{}{version}}}}).Save(m).Error
	if err != nil {
		glog.Warningf("error saving model %s, error: %s, %+v", m.GetID(), err, m)
		m.DecrementVersion()
		return false
	}
	return true
}

// Return the number of each item object by status.
func (db *GormRepo) GetCountByStatus(ctx context.Context, id string) (map[Status]int, error) {
	ctx, cancel := db.WithTimeout(ctx)
	defer cancel()
	rows, err := db.WithContext(ctx).Model(&Item{}).Select("status, COUNT(*)").Where("partition_id = ?", id).Group("status").Rows()
	if err != nil {
		return nil, err
	}

	var (
		status      Status
		count       int
		leaseCounts = map[Status]int{}
	)
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		leaseCounts[status] = count
	}
	return leaseCounts, nil
}

func (db *GormRepo) Transaction(ctx context.Context, f func(db *GormRepo) error) error {
	ctx, cancel := db.WithTimeout(ctx)
	defer cancel()
	return db.WithContext(ctx).Transaction(func(gdb *gorm.DB) error {
		return f(&GormRepo{DB: gdb, Timeout: db.Timeout})
	})
}
