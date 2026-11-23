//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_kubernetes.go -package=mock
package domain

import "context"

type KubernetesClient interface {
	ListNodePods(ctx context.Context) ([]Pod, error)
}
