package repository

import (
	"context"

	"github.com/helthtech/public-max-bot/internal/model"
	"gorm.io/gorm"
)

type ChatRepository struct {
	db *gorm.DB
}

func NewChatRepository(db *gorm.DB) *ChatRepository {
	return &ChatRepository{db: db}
}

func (r *ChatRepository) FindByMaxUserID(ctx context.Context, maxUserID string) (*model.Chat, error) {
	var chat model.Chat
	err := r.db.WithContext(ctx).Where("max_user_id = ?", maxUserID).First(&chat).Error
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

func (r *ChatRepository) FindByUserID(ctx context.Context, userID string) (*model.Chat, error) {
	var chat model.Chat
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&chat).Error
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

func (r *ChatRepository) Upsert(ctx context.Context, chat *model.Chat) error {
	return r.db.WithContext(ctx).Save(chat).Error
}
