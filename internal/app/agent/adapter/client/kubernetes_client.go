package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// KubernetesClient は Kubernetes API から自分のノードの Pod を読む (daemon 型)。
// トークンと CA はリクエストのたびにファイルから読む。ServiceAccount のトークンは kubelet が入れ替えるため
type KubernetesClient struct {
	api       string
	node      string
	tokenFile string
	caFile    string
	timeout   time.Duration
}

func NewKubernetesClient(cfg *config.Config) (*KubernetesClient, error) {
	return &KubernetesClient{api: strings.TrimRight(cfg.KubernetesAPI, "/"), node: cfg.NodeName, tokenFile: cfg.KubernetesToken, caFile: cfg.KubernetesCAFile, timeout: cfg.RequestTimeout}, nil
}

type podList struct {
	Items []struct {
		Metadata struct {
			UID         string            `json:"uid"`
			Name        string            `json:"name"`
			Namespace   string            `json:"namespace"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct {
				Name string `json:"name"`
			} `json:"containers"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			PodIP             string `json:"podIP"`
			ContainerStatuses []struct {
				Name        string `json:"name"`
				ContainerID string `json:"containerID"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func (c *KubernetesClient) ListNodePods(ctx context.Context) ([]domain.Pod, error) {
	hc, token, err := c.httpClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	q := url.Values{"fieldSelector": {"spec.nodeName=" + c.node}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+"/api/v1/pods?"+q.Encode(), nil)
	if err != nil {
		return nil, domain.ErrInvalidArgument.Wrap(err, "build kubernetes request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "list pods")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, domain.ErrInternal.With(fmt.Sprintf("list pods returned %d: %s", res.StatusCode, strings.TrimSpace(string(msg))))
	}
	var list podList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, domain.ErrInternal.Wrap(err, "decode pods")
	}
	pods := make([]domain.Pod, 0, len(list.Items))
	for _, it := range list.Items {
		p := domain.Pod{
			UID:          it.Metadata.UID,
			Namespace:    it.Metadata.Namespace,
			Name:         it.Metadata.Name,
			IP:           it.Status.PodIP,
			Running:      it.Status.Phase == "Running",
			Labels:       it.Metadata.Labels,
			Annotations:  it.Metadata.Annotations,
			ContainerIDs: map[string]string{},
		}
		for _, ct := range it.Spec.Containers {
			p.Containers = append(p.Containers, ct.Name)
		}
		for _, cs := range it.Status.ContainerStatuses {
			p.ContainerIDs[cs.Name] = cs.ContainerID
		}
		pods = append(pods, p)
	}
	return pods, nil
}

func (c *KubernetesClient) httpClient() (*http.Client, string, error) {
	token, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return nil, "", domain.ErrInternal.Wrap(err, "read the service account token")
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca, err := os.ReadFile(c.caFile); err == nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		tlsCfg.RootCAs = pool
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}, strings.TrimSpace(string(token)), nil
}
