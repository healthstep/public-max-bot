package model

import "time"

type Chat struct {
	ID                string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ProvisionalUserID *string   `gorm:"type:uuid" json:"provisional_user_id,omitempty"`
	UserID            *string   `gorm:"type:uuid" json:"user_id,omitempty"`
	MaxUserID         string    `gorm:"type:text;uniqueIndex" json:"max_user_id"`
	ChatID            *string   `gorm:"type:text" json:"chat_id,omitempty"`
	Username          *string   `gorm:"type:text" json:"username,omitempty"`
	AuthKey           *string   `gorm:"type:text" json:"-"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (Chat) TableName() string { return "max_bot.chats" }
