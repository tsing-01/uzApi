package handler

import (
	"testing"
)

func TestFlattenIntegrationModelsDedupesAndMergesGroups(t *testing.T) {
	pricing := &userSupportedModelPricing{BillingMode: "token"}
	channels := []userAvailableChannel{
		{
			Name: "ChannelB",
			Platforms: []userChannelPlatformSection{
				{
					Platform: "openai",
					Groups:   []userAvailableGroup{{ID: 2}, {ID: 1}},
					SupportedModels: []userSupportedModel{
						{Name: "gpt-5", Platform: "openai", Pricing: nil},
						{Name: "gpt-4o", Platform: "openai", Pricing: pricing},
					},
				},
			},
		},
		{
			Name: "ChannelA",
			Platforms: []userChannelPlatformSection{
				{
					Platform: "openai",
					Groups:   []userAvailableGroup{{ID: 2}, {ID: 3}},
					SupportedModels: []userSupportedModel{
						{Name: "gpt-5", Platform: "openai", Pricing: pricing},
					},
				},
				{
					Platform: "anthropic",
					Groups:   []userAvailableGroup{{ID: 4}},
					SupportedModels: []userSupportedModel{
						{Name: "claude-opus-5", Platform: "anthropic", Pricing: nil},
					},
				},
			},
		},
	}

	models := flattenIntegrationModels(channels)

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d: %+v", len(models), models)
	}
	if models[0].Platform != "anthropic" || models[0].Name != "claude-opus-5" {
		t.Fatalf("expected anthropic model first, got %+v", models[0])
	}
	if models[1].Name != "gpt-4o" || models[2].Name != "gpt-5" {
		t.Fatalf("expected models sorted by platform then name, got %+v", models)
	}

	gpt5 := models[2]
	if len(gpt5.GroupIDs) != 3 || gpt5.GroupIDs[0] != 1 || gpt5.GroupIDs[1] != 2 || gpt5.GroupIDs[2] != 3 {
		t.Fatalf("expected merged sorted group ids [1 2 3], got %+v", gpt5.GroupIDs)
	}
	if gpt5.Pricing != pricing {
		t.Fatalf("expected pricing to be backfilled from the later non-nil entry")
	}
}

func TestFlattenIntegrationModelsEmptyChannels(t *testing.T) {
	models := flattenIntegrationModels(nil)
	if models == nil || len(models) != 0 {
		t.Fatalf("expected empty non-nil slice, got %+v", models)
	}
}
