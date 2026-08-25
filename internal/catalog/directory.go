package catalog

import (
	"sort"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
)

// Directory is the versioned lineage and incubation-rule catalog. It answers
// lock-snapshot validation and qualification queries, and it is immutable
// after construction so a locked task never observes a retroactive rule change.
type Directory struct {
	rules map[string][]domain.CatalogRule // lineage code -> versions, ascending
}

// NewDirectory builds a directory from a flat slice of rules, grouping them by
// lineage code and sorting each group by rule version ascending.
func NewDirectory(rules []domain.CatalogRule) *Directory {
	d := &Directory{rules: make(map[string][]domain.CatalogRule)}
	for _, r := range rules {
		d.rules[r.LineageCode] = append(d.rules[r.LineageCode], r)
	}
	for k := range d.rules {
		sort.Slice(d.rules[k], func(i, j int) bool {
			return d.rules[k][i].RuleVersion < d.rules[k][j].RuleVersion
		})
	}
	return d
}

// Lookup returns the rule for a lineage at an exact version.
func (d *Directory) Lookup(lineage string, version int64) (domain.CatalogRule, bool) {
	versions, ok := d.rules[lineage]
	if !ok {
		return domain.CatalogRule{}, false
	}
	for _, r := range versions {
		if r.RuleVersion == version {
			return r, true
		}
	}
	return domain.CatalogRule{}, false
}

// Current returns the latest rule for a lineage.
func (d *Directory) Current(lineage string) (domain.CatalogRule, bool) {
	versions, ok := d.rules[lineage]
	if !ok || len(versions) == 0 {
		return domain.CatalogRule{}, false
	}
	return versions[len(versions)-1], true
}

// HasLineage reports whether the lineage is known at all.
func (d *Directory) HasLineage(lineage string) bool {
	_, ok := d.rules[lineage]
	return ok
}

// SeedRules returns the default production catalog used by the runnable entry
// point and the deterministic tests. It models a single lineage whose rule
// version 1 permits two batches, five incubation day ages, and a full set of
// fixed-point thresholds and role qualification / exclusion lists.
func SeedRules() []domain.CatalogRule {
	th := func(s string) fixedpoint.Value {
		v, err := fixedpoint.Parse(s, 2)
		if err != nil {
			panic(err)
		}
		return v
	}
	return []domain.CatalogRule{
		{
			LineageCode:       "lineage-001",
			RuleVersion:       1,
			AllowedBatches:    []string{"batch-001", "batch-002"},
			IncubationDayAges: []int{1, 2, 3, 4, 5},
			SampleSize:        100,
			Thresholds: domain.Thresholds{
				MinHatchRate:   th("80.00"),
				MaxDeadEggRate: th("5.00"),
				MaxMoisture:    th("12.00"),
				MinTemperature: th("20.00"),
				MaxTemperature: th("30.00"),
				MinHumidity:    th("60.00"),
				MaxHumidity:    th("90.00"),
				MaxSporeCount:  th("0.00"),
			},
			RoleQualifications: map[string][]string{
				"production": {"person-a", "person-b"},
				"review":     {"person-c", "person-d"},
			},
			MutualExclusions: []domain.RolePair{{RoleA: "production", RoleB: "review"}},
		},
	}
}
