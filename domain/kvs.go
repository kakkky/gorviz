package domain

type InMemoryKVS interface {
	Get(key string) (any, bool)
	Set(key string, value any)
}
