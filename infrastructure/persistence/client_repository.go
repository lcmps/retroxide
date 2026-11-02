package persistence

import (
	"context"

	authdomain "github.com/lcmps/retroxide/domain/auth"
)

func (repositories *Repositories) FindByTokenHash(ctx context.Context, tokenHash string) (*authdomain.Client, error) {
	model, err := takeOne[ClientModel](repositories.DB.WithContext(ctx).Where("token_hash = ?", tokenHash), "finding client by token hash")
	return mapOptional(model, err, infallible(clientEntity))
}

func (repositories *Repositories) FindByUserID(ctx context.Context, userID string) ([]authdomain.Client, error) {
	var models []ClientModel
	if err := repositories.DB.WithContext(ctx).Where("user_id = ?", userID).Order("id ASC").Find(&models).Error; err != nil {
		return nil, databaseError("listing clients", err)
	}
	clients := make([]authdomain.Client, 0, len(models))
	for _, model := range models {
		clients = append(clients, clientEntity(model))
	}
	return clients, nil
}

func (repositories *Repositories) SaveClient(ctx context.Context, client authdomain.Client) error {
	model := ClientModel{ID: client.ID, UserID: client.UserID, Name: client.Name, TokenHash: client.TokenHash}
	if err := repositories.DB.WithContext(ctx).Create(&model).Error; err != nil {
		return databaseError("saving client", err)
	}
	return nil
}

func (repositories *Repositories) DeleteClient(ctx context.Context, clientID string) error {
	return expectDeleted(repositories.DB.WithContext(ctx).Where("id = ?", clientID).Delete(&ClientModel{}), "deleting client")
}

func clientEntity(model ClientModel) authdomain.Client {
	return authdomain.Client{ID: model.ID, UserID: model.UserID, Name: model.Name, TokenHash: model.TokenHash}
}
