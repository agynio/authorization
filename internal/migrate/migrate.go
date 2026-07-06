// Package migrate provisions the OpenFGA store and authorization model and
// publishes the resulting store/model IDs into a Kubernetes Secret so the
// authorization service (a separate Deployment) can consume them.
//
// This replaces the previous Terraform-based provisioning (the openfga
// provider in //terraform): the model is now shipped and applied by the same
// image as the service, as a Helm-managed migration Job.
package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/agynio/authorization/internal/authz"
	openfgaclient "github.com/openfga/go-sdk/client"
	"github.com/openfga/language/pkg/go/transformer"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultStoreName = "agyn-platform"
	storeIDSecretKey = "OPENFGA_STORE_ID"
	modelIDSecretKey = "OPENFGA_MODEL_ID"
	namespaceFile    = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	storeListPage    = 100
)

// Config is the migration configuration, read from the environment.
type Config struct {
	OpenFGAAPIURL string
	StoreName     string
	OutputSecret  string // name of the Secret to write store/model IDs into
	Namespace     string // namespace of the output Secret
}

func configFromEnv() (Config, error) {
	cfg := Config{
		OpenFGAAPIURL: strings.TrimSpace(os.Getenv("OPENFGA_API_URL")),
		StoreName:     strings.TrimSpace(os.Getenv("OPENFGA_STORE_NAME")),
		OutputSecret:  strings.TrimSpace(os.Getenv("OPENFGA_OUTPUT_SECRET")),
		Namespace:     strings.TrimSpace(os.Getenv("POD_NAMESPACE")),
	}
	if cfg.OpenFGAAPIURL == "" {
		return Config{}, fmt.Errorf("OPENFGA_API_URL must be set")
	}
	if cfg.StoreName == "" {
		cfg.StoreName = defaultStoreName
	}
	if cfg.OutputSecret == "" {
		return Config{}, fmt.Errorf("OPENFGA_OUTPUT_SECRET must be set")
	}
	if cfg.Namespace == "" {
		ns, err := os.ReadFile(namespaceFile)
		if err != nil {
			return Config{}, fmt.Errorf("POD_NAMESPACE unset and cannot read %s: %w", namespaceFile, err)
		}
		cfg.Namespace = strings.TrimSpace(string(ns))
	}
	return cfg, nil
}

// Run executes the migration: ensure the store exists, apply the authorization
// model (only writing a new version when the DSL changed), and upsert the
// output Secret. It is idempotent.
func Run(ctx context.Context) error {
	cfg, err := configFromEnv()
	if err != nil {
		return err
	}

	client, err := openfgaclient.NewSdkClient(&openfgaclient.ClientConfiguration{
		ApiUrl: cfg.OpenFGAAPIURL,
	})
	if err != nil {
		return fmt.Errorf("create OpenFGA client: %w", err)
	}

	storeID, err := ensureStore(ctx, client, cfg.StoreName)
	if err != nil {
		return err
	}
	if err := client.SetStoreId(storeID); err != nil {
		return fmt.Errorf("set store id: %w", err)
	}

	modelID, err := ensureModel(ctx, client)
	if err != nil {
		return err
	}

	if err := writeOutputSecret(ctx, cfg, storeID, modelID); err != nil {
		return err
	}

	fmt.Printf("migrate: store=%s model=%s secret=%s/%s\n", storeID, modelID, cfg.Namespace, cfg.OutputSecret)
	return nil
}

// ensureStore returns the id of the store named storeName, creating it if it
// does not already exist.
func ensureStore(ctx context.Context, client *openfgaclient.OpenFgaClient, storeName string) (string, error) {
	var continuation string
	for {
		opts := openfgaclient.ClientListStoresOptions{PageSize: ptr(int32(storeListPage))}
		if continuation != "" {
			opts.ContinuationToken = &continuation
		}
		resp, err := client.ListStores(ctx).Options(opts).Execute()
		if err != nil {
			return "", fmt.Errorf("list stores: %w", err)
		}
		for _, store := range resp.GetStores() {
			if store.GetName() == storeName {
				return store.GetId(), nil
			}
		}
		continuation = resp.GetContinuationToken()
		if continuation == "" {
			break
		}
	}

	created, err := client.CreateStore(ctx).
		Body(openfgaclient.ClientCreateStoreRequest{Name: storeName}).
		Execute()
	if err != nil {
		return "", fmt.Errorf("create store %q: %w", storeName, err)
	}
	return created.GetId(), nil
}

// ensureModel applies the embedded DSL model, reusing the latest model when it
// is identical so repeated migrations don't churn the model id.
func ensureModel(ctx context.Context, client *openfgaclient.OpenFgaClient) (string, error) {
	modelJSON, err := transformer.TransformDSLToJSON(authz.ModelDSL)
	if err != nil {
		return "", fmt.Errorf("transform model DSL: %w", err)
	}

	var body openfgaclient.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(modelJSON), &body); err != nil {
		return "", fmt.Errorf("parse transformed model: %w", err)
	}

	if latest, err := client.ReadLatestAuthorizationModel(ctx).Execute(); err == nil && latest.AuthorizationModel != nil {
		if existing, mErr := json.Marshal(latest.AuthorizationModel); mErr == nil {
			if same, cErr := sameModel(modelJSON, string(existing)); cErr == nil && same {
				return latest.AuthorizationModel.GetId(), nil
			}
		}
	}

	written, err := client.WriteAuthorizationModel(ctx).Body(body).Execute()
	if err != nil {
		return "", fmt.Errorf("write authorization model: %w", err)
	}
	return written.GetAuthorizationModelId(), nil
}

// sameModel reports whether two authorization models (as JSON) are semantically
// identical. It compares their canonical DSL forms rather than the JSON:
// OpenFGA hydrates a stored model with zero-value fields the transformer omits
// (empty module/condition strings, empty relation metadata, an empty conditions
// map), so a structural JSON compare reports a spurious difference on every run
// and churns a new model on each sync. Round-tripping both sides through the DSL
// transformer strips that noise, leaving a stable comparison.
func sameModel(a, b string) (bool, error) {
	da, err := transformer.TransformJSONStringToDSL(a)
	if err != nil {
		return false, fmt.Errorf("normalize model a: %w", err)
	}
	db, err := transformer.TransformJSONStringToDSL(b)
	if err != nil {
		return false, fmt.Errorf("normalize model b: %w", err)
	}
	return *da == *db, nil
}

// writeOutputSecret upserts the output Secret with the store/model IDs using
// in-cluster credentials.
func writeOutputSecret(ctx context.Context, cfg Config, storeID, modelID string) error {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("in-cluster config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	secrets := clientset.CoreV1().Secrets(cfg.Namespace)
	data := map[string]string{
		storeIDSecretKey: storeID,
		modelIDSecretKey: modelID,
	}

	existing, err := secrets.Get(ctx, cfg.OutputSecret, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: cfg.OutputSecret, Namespace: cfg.Namespace},
			StringData: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create secret %s: %w", cfg.OutputSecret, err)
		}
	case err != nil:
		return fmt.Errorf("get secret %s: %w", cfg.OutputSecret, err)
	default:
		existing.StringData = data
		if _, err := secrets.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update secret %s: %w", cfg.OutputSecret, err)
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
