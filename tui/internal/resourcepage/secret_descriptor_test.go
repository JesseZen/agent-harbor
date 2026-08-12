package resourcepage

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/resourcepage/generated"
)

func TestSecretActionDescriptorExpandsOneOfChildren(t *testing.T) {
	desc, ok := generated.Descriptors[generated.ResourceCredential]
	if !ok {
		t.Fatal("missing credential descriptor")
	}
	var secretField *generated.FieldDescriptor
	for i := range desc.Fields {
		if desc.Fields[i].Name == "secret_action" {
			secretField = &desc.Fields[i]
			break
		}
	}
	if secretField == nil {
		t.Fatal("missing secret_action field")
	}
	if secretField.Kind != generated.FieldKindSecret {
		t.Fatalf("secret_action kind = %q, want secret", secretField.Kind)
	}
	if len(secretField.Children) == 0 {
		t.Fatal("secret_action must expand oneOf children (mode/stage_id/ref)")
	}
	childNames := make(map[string]struct{})
	for _, child := range secretField.Children {
		childNames[child.Name] = struct{}{}
	}
	for _, want := range []string{"mode", "stage_id", "ref"} {
		if _, ok := childNames[want]; !ok {
			t.Fatalf("missing child %q in %#v", want, secretField.Children)
		}
	}
}
