// Package instance は、実行環境 (hostname・Cloud Run) から agent の名前を決める
package instance

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

var cloudRunInstanceIDURL = "http://metadata.google.internal/computeMetadata/v1/instance/id"

// Name はレプリカごとに違う agent の名前を返す。server は同じ名前の agent を 1 つの Session に束ねるので、レプリカで重なると別のプロセスの trace が混ざる。
// name を指定したらそれを使い、省いたら hostname にする。Cloud Run ではどのインスタンスも hostname が localhost なので、name (省いたらサービス名) に instance ID を足す
func Name(ctx context.Context, name string) (string, error) {
	if service := os.Getenv("K_SERVICE"); service != "" {
		id, err := cloudRunInstanceID(ctx)
		if err != nil {
			return "", fmt.Errorf("get the Cloud Run instance ID: %w", err)
		}
		return cmp.Or(name, service) + "/" + id, nil
	}
	if name != "" {
		return name, nil
	}
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	if host == "localhost" {
		return host + "/" + rand.Text()[:8], nil
	}
	return host, nil
}

// cloudRunInstanceID は instance ID を縮めて返す。そのままでは長すぎて画面に出せず、先頭だけではインスタンス間で重ならないとは限らないのでハッシュする
func cloudRunInstanceID(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudRunInstanceIDURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata server: %s", res.Status)
	}
	id, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(id)
	return hex.EncodeToString(sum[:6]), nil
}

// ForTarget は daemon 型で複数の対象を見るときの名前。対象ごとに別の名前になるよう host:port を足す
func ForTarget(name, target string) string {
	host := target
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		host = u.Host
	}
	return name + "/" + host
}
