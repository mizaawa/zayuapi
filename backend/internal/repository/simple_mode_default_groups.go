package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const simpleModeDefaultGroupDescription = "Auto-created default group"

func ensureSimpleModeDefaultGroups(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	if err := backfillSimpleModeGrokDefaultImageGeneration(ctx, client); err != nil {
		return err
	}

	// Antigravity is retired, so simple mode no longer seeds its groups.
	for _, platform := range []string{
		service.PlatformAnthropic,
		service.PlatformOpenAI,
		service.PlatformGemini,
		service.PlatformGrok,
	} {
		name := platform + "-default"
		if err := createGroupIfNotExists(ctx, client, name, platform); err != nil {
			return err
		}
	}

	return nil
}

// ensureCustomDefaultGroup is intentionally independent from simple mode.
// It seeds the first custom route but respects operator-managed group changes.
func ensureCustomDefaultGroup(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	const name = service.PlatformCustom + "-default"
	// Older builds could persist this group with the account platform value
	// (`custom`). Custom accounts are validated against the legacy composite
	// group discriminator, so repair that row in place during startup.
	updated, err := client.Group.Update().
		Where(group.NameEQ(name), group.DeletedAtIsNil(), group.PlatformNEQ(service.PlatformComposite)).
		SetPlatform(service.PlatformComposite).
		SetAllowImageGeneration(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("repair custom default group: %w", err)
	}
	if updated > 0 {
		return nil
	}

	// Names are editable and deletion is an operator decision. Include deleted
	// rows only for this bootstrap check so upgrades do not recreate the group.
	exists, err := client.Group.Query().
		Where(group.PlatformIn(service.PlatformCustom, service.PlatformComposite)).
		Exist(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return fmt.Errorf("check custom group history: %w", err)
	}
	if exists {
		return nil
	}
	return createGroupIfNotExists(ctx, client, name, service.PlatformComposite)
}

func createGroupIfNotExists(ctx context.Context, client *dbent.Client, name, platform string) error {
	exists, err := client.Group.Query().
		Where(group.NameEQ(name), group.DeletedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check group exists %s: %w", name, err)
	}
	if exists {
		return nil
	}

	_, err = client.Group.Create().
		SetName(name).
		SetDescription(simpleModeDefaultGroupDescription).
		SetPlatform(platform).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1.0).
		SetIsExclusive(false).
		SetAllowImageGeneration(platform == service.PlatformGrok || platform == service.PlatformComposite).
		Save(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			// Concurrent server startups may race on creation; treat as success.
			return nil
		}
		return fmt.Errorf("create default group %s: %w", name, err)
	}
	return nil
}

func backfillSimpleModeGrokDefaultImageGeneration(ctx context.Context, client *dbent.Client) error {
	_, err := client.Group.Update().
		Where(
			group.NameEQ(service.PlatformGrok+"-default"),
			group.PlatformEQ(service.PlatformGrok),
			group.DescriptionEQ(simpleModeDefaultGroupDescription),
			group.StatusEQ(service.StatusActive),
			group.AllowImageGenerationEQ(false),
			group.DeletedAtIsNil(),
		).
		SetAllowImageGeneration(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("backfill auto-created grok default image generation: %w", err)
	}
	return nil
}
