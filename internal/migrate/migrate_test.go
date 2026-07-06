package migrate

import (
	"testing"

	"github.com/agynio/authorization/internal/authz"
	"github.com/openfga/language/pkg/go/transformer"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestSameModelIgnoresServerHydration guards the model-churn regression: OpenFGA
// stores and returns a model with every zero-value field populated (empty
// module/condition strings, empty relation metadata, an empty conditions map),
// whereas the transformer emits a minimal JSON. A structural JSON compare
// therefore reported a spurious difference on every migration and wrote a new
// model each sync. sameModel must treat the hydrated and minimal forms as equal.
//
// The hydrated form is generated from the same embedded DSL (proto + protojson
// with EmitUnpopulated mirrors the server's hydration), so this test regenerates
// itself when the model changes instead of pinning a brittle fixture.
func TestSameModelIgnoresServerHydration(t *testing.T) {
	desired, err := transformer.TransformDSLToJSON(authz.ModelDSL)
	if err != nil {
		t.Fatalf("transform embedded DSL: %v", err)
	}

	proto, err := transformer.TransformDSLToProto(authz.ModelDSL)
	if err != nil {
		t.Fatalf("transform embedded DSL to proto: %v", err)
	}
	hydrated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(proto)
	if err != nil {
		t.Fatalf("marshal hydrated model: %v", err)
	}

	same, err := sameModel(desired, string(hydrated))
	if err != nil {
		t.Fatalf("sameModel: %v", err)
	}
	if !same {
		t.Fatal("sameModel reported the minimal and server-hydrated forms of the same model as different; migration would churn a new model every sync")
	}
}

// TestSameModelDetectsChange ensures the DSL-normalizing compare does not
// over-normalize: two genuinely different models must compare as not equal, so a
// real DSL change still produces a new authorization model.
func TestSameModelDetectsChange(t *testing.T) {
	viewer, err := transformer.TransformDSLToJSON("model\n  schema 1.1\ntype user\ntype doc\n  relations\n    define viewer: [user]\n")
	if err != nil {
		t.Fatalf("transform viewer model: %v", err)
	}
	editor, err := transformer.TransformDSLToJSON("model\n  schema 1.1\ntype user\ntype doc\n  relations\n    define editor: [user]\n")
	if err != nil {
		t.Fatalf("transform editor model: %v", err)
	}

	if same, err := sameModel(viewer, viewer); err != nil || !same {
		t.Fatalf("sameModel(x, x) = %v, %v; want true, nil", same, err)
	}
	if same, err := sameModel(viewer, editor); err != nil {
		t.Fatalf("sameModel(viewer, editor) error: %v", err)
	} else if same {
		t.Fatal("sameModel treated two different models as equal; a real DSL change would not be applied")
	}
}
