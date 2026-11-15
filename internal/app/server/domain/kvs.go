package domain

type InMemoryKVS interface {
	Get(key string) (any, bool)
	Set(key string, value any)
}

// KVSGet は key の値を T として取り出す。ないか型が違えば false
func KVSGet[T any](kvs InMemoryKVS, key string) (T, bool) {
	var zero T
	v, ok := kvs.Get(key)
	if !ok {
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}

// KVSGetOr は key の値を T として取り出す。ないか型が違えば fallback を返す
func KVSGetOr[T any](kvs InMemoryKVS, key string, fallback T) T {
	if t, ok := KVSGet[T](kvs, key); ok {
		return t
	}
	return fallback
}
