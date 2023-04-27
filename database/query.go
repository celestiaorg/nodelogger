package database

import (
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/tools/cache"
	"gorm.io/gorm"
)

func Query(db *gorm.DB, SQL string, rows interface{}) error {
	return db.Raw(SQL).Scan(rows).Error
}

func CachedQuery(db *gorm.DB, SQL string, rows interface{}) error {

	sqlHash := fmt.Sprint(sha256.Sum256([]byte(SQL)))

	diskStorage := cache.New()

	err := diskStorage.ReadAny(sqlHash, rows)
	if err != nil {
		if err := Query(db, SQL, rows); err != nil {
			return err
		}
		return diskStorage.StoreAny(sqlHash, rows)
	}

	return nil
}

func RemoveCachedQuery(SQL string) error {

	sqlHash := fmt.Sprint(sha256.Sum256([]byte(SQL)))

	diskStorage := cache.New()
	return diskStorage.Remove(sqlHash)
}
