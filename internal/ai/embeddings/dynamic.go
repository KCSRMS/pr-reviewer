package embeddings

import (
	"context"
	"errors"
)

// ErrNoProviderConfigured is returned by a DynamicEmbedder's Resolver when no
// provider in the database currently has embeddings enabled.
var ErrNoProviderConfigured = errors.New("no embedding provider configured")

// Resolver builds the current Embedder from live application state (typically
// the provider_configs table), returning ErrNoProviderConfigured if nothing is
// configured yet.
type Resolver func() (Embedder, error)

// DynamicEmbedder wraps a Resolver so provider changes made through the UI
// (add, edit, enable, disable) take effect on the next call — no server
// restart required. Every call re-resolves; this is cheap relative to the
// network call to the embedding API it wraps.
type DynamicEmbedder struct {
	resolve Resolver
}

func NewDynamicEmbedder(resolve Resolver) *DynamicEmbedder {
	return &DynamicEmbedder{resolve: resolve}
}

func (d *DynamicEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e, err := d.resolve()
	if err != nil {
		return nil, err
	}
	return e.Embed(ctx, text)
}

func (d *DynamicEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e, err := d.resolve()
	if err != nil {
		return nil, err
	}
	return e.EmbedBatch(ctx, texts)
}

func (d *DynamicEmbedder) Dim() int {
	e, err := d.resolve()
	if err != nil {
		return 0
	}
	return e.Dim()
}

func (d *DynamicEmbedder) ID() string {
	e, err := d.resolve()
	if err != nil {
		return ""
	}
	return e.ID()
}

// Ready reports whether a provider is currently resolvable. Used for
// pre-flight availability checks, where Dim/ID's zero-value-on-error isn't
// enough to distinguish "unconfigured" from "configured but unusual".
func (d *DynamicEmbedder) Ready() bool {
	_, err := d.resolve()
	return err == nil
}
