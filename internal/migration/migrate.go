package migration

import (
	"github.com/helthtech/public-max-bot/internal/model"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	db.Exec("CREATE SCHEMA IF NOT EXISTS max_bot")
	return db.AutoMigrate(&model.Chat{})
}
