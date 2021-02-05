package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"dev.azure.com/CSECodeHub/378940+-+PWC+Health+OSIC+Platform+-+DICOM/SQLStateProcessor/internal/processors/httprocessor"
	"dev.azure.com/CSECodeHub/378940+-+PWC+Health+OSIC+Platform+-+DICOM/SQLStateProcessor/internal/state"
	"github.com/etherlabsio/healthcheck"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var (
	target          = flag.String("target", "", "target to send post requests to")
	sqlConnStr      = flag.String("sql_connection", "", "sql connection string")
	local           = flag.Bool("local", false, "whether to use a local sqlite3 server")
	pollInterval    = flag.Duration("poll_interval", 10*time.Second, "how long to wait to poll sql")
	batchSize       = flag.Int("batch_size", 50, "number of states to process simultaneously")
	tablePrefix     = flag.String("table_prefix", "", "the table prefix to use, useful for namespacing or running tests. Not compatible when setting the err_table_schema flag")
	healthcheckAddr = flag.String("healthcheck_address", ":8080", "healthcheck address and port")

	dbLogLevel gormLogFlag
)

func init() {
	flag.Var(&dbLogLevel, "db_log_level", "database log level")
	flag.Parse()
}

// helpers for gorm flags
type gormLogFlag struct {
	value string
	level logger.LogLevel
}

func (g *gormLogFlag) String() string {
	return g.value
}

func (g *gormLogFlag) Set(value string) error {
	var gormValues = map[string]logger.LogLevel{
		"silent": logger.Silent,
		"error":  logger.Error,
		"warn":   logger.Warn,
		"info":   logger.Info,
	}
	if l, ok := gormValues[value]; ok {
		g.value = value
		g.level = l
		return nil
	}
	return fmt.Errorf("unknown gorm log level: %s", value)
}

func main() {
	var db *gorm.DB
	var err error

	gConf := &gorm.Config{
		Logger: logger.Default.LogMode(dbLogLevel.level),
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: *tablePrefix,
		},
	}
	if *local {
		glog.Info("Attempting to connect to local db")
		db, err = gorm.Open(sqlite.Open("test.db"), gConf)
	} else {
		glog.Info("Attempting to connect to remote db")
		db, err = gorm.Open(sqlserver.Open(*sqlConnStr), gConf)
	}

	if err != nil {
		panic("failed to connect database")
	}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	w := state.Watcher{
		Repo: &state.GormRepo{DB: db},
		Processor: &httprocessor.Processor{
			Client: netClient,
			Target: *target,
		},
		PollInterval: *pollInterval,
		BatchSize:    *batchSize,
	}

	r := mux.NewRouter()

	r.Handle("/healthcheck", healthcheck.Handler(healthcheck.WithTimeout(5*time.Second),
		healthcheck.WithChecker(
			"state_processor", healthcheck.CheckerFunc(w.Healthcheck),
		)))

	if err := w.AutoMigrate(); err != nil {
		glog.Fatalf("failed to migrate DB: %s ", err)
	}

	go w.Start(context.Background())
	glog.Info(http.ListenAndServe(*healthcheckAddr, r))
}
