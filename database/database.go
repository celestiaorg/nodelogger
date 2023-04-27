package database

import (
	"log"
	"os"
	"time"

	"github.com/celestiaorg/nodelogger/database/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Init(connStr string) (*gorm.DB, error) {

	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             10 * time.Second, // Slow SQL threshold
			LogLevel:                  logger.Silent,    // Log level
			IgnoreRecordNotFoundError: true,             // Ignore ErrRecordNotFound error for logger
			// ParameterizedQueries:      true,          // Don't include params in the SQL log
			// Colorful:                  false,         // Disable color
		},
	)

	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{Logger: newLogger})
	if err != nil {
		return nil, err
	}
	err = db.AutoMigrate(&models.CelestiaNode{})

	return db, err
}
