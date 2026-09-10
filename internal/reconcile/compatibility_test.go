package reconcile

import (
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

func TestNewFieldsLeaveLegacyDeclarationsUnmanaged(t *testing.T) {
	actual := model.Agent{ServiceTier: "priority", SystemKey: "mika", ConversationStarters: []model.ConversationStarter{{Label: "Review", Prompt: "Review."}}}
	fields, err := (Reconciler{}).diffAgent(model.AgentSpec{}, "", actual, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"serviceTier", "systemKey", "conversationStarters"} {
		if slices.Contains(fields, name) {
			t.Fatalf("legacy declaration started managing %s", name)
		}
	}
	if err := validateObservedOnlyChanges(model.AgentSpec{}, actual, nil); err != nil {
		t.Fatal(err)
	}
}

func TestServiceTierDiffAndInputIncludeExplicitClear(t *testing.T) {
	for _, value := range []string{"", "default", "priority"} {
		desired := model.AgentSpec{ServiceTier: &value}
		for _, actual := range []string{"", "default", "priority"} {
			fields, err := (Reconciler{}).diffAgent(desired, "", model.Agent{ServiceTier: actual}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(fields, "serviceTier") != (value != actual) {
				t.Fatalf("desired=%q actual=%q fields=%v", value, actual, fields)
			}
		}
		if in := agentInput(desired, "r"); in.ServiceTier == nil || *in.ServiceTier != value {
			t.Fatal("tier lost in agent input")
		}
	}
	if !slices.Contains(baseAgentFields([]string{"serviceTier"}), "serviceTier") || !requiresActiveAgent([]string{"serviceTier"}) {
		t.Fatal("service-tier updates must reach the base CLI mutation, including archived agents")
	}
}

func TestReadOnlyAgentFieldsCannotBeCreatedOrChanged(t *testing.T) {
	key := "mika"
	starters := []model.ConversationStarter{{Label: "Review", Prompt: "Review."}}
	for _, desired := range []model.AgentSpec{
		{Unbound: true}, {SystemKey: &key}, {ConversationStarters: &starters},
	} {
		if err := validateObservedOnlyOnCreate(desired); err == nil {
			t.Fatal("unsafe create allowed")
		}
		if err := validateObservedOnlyChanges(desired, model.Agent{RuntimeID: "r"}, nil); err == nil {
			t.Fatal("unsafe mutation allowed")
		}
	}
	// An unchanged product-managed agent is a valid in-place reconciliation target.
	if err := validateObservedOnlyChanges(model.AgentSpec{Unbound: true, SystemKey: &key, ConversationStarters: &starters}, model.Agent{SystemKey: key, ConversationStarters: starters}, nil); err != nil {
		t.Fatal(err)
	}
	// Binding an unbound agent to an explicitly selected runtime is supported.
	if err := validateObservedOnlyChanges(model.AgentSpec{RuntimeRef: "r"}, model.Agent{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMaskedGatewayTokenBlocksReconciliation(t *testing.T) {
	_, err := (Reconciler{}).diffAgent(model.AgentSpec{}, "", model.Agent{RuntimeConfig: map[string]any{"gateway": map[string]any{"token": "***"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "masked") {
		t.Fatalf("expected masking error: %v", err)
	}
}
