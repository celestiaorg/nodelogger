package database

import (
	"github.com/celestiaorg/nodelogger/database/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Init(connStr string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	err = db.AutoMigrate(&models.CelestiaNode{})

	return db, err
}
