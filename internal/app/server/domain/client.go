//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_client.go -package=mock
package domain

import "context"

type NotificationClient interface {
	Send(ctx context.Context, dest Destination, n Notification) error
}
