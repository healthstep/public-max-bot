package bot

import (
	"fmt"
	"sort"
	"strings"

	healthpb "github.com/helthtech/core-health/pkg/proto/health"
)

func formatProgressGroupedMarkdown(prog *healthpb.GetProgressResponse, entries []*healthpb.UserCriterionEntry, groups []*healthpb.CriterionGroup) string {
	var b strings.Builder
	b.WriteString("📊 **Мой прогресс**\n\n")
	b.WriteString(fmt.Sprintf("Уровень: **%s**\n", prog.GetLevelLabel()))
	pct := prog.GetPercent()
	bar := progressBar(pct)
	b.WriteString(fmt.Sprintf("%s %.0f%%\n", bar, pct))
	b.WriteString(fmt.Sprintf("Заполнено: %d/%d критериев\n", prog.GetFilled(), prog.GetTotal()))

	if len(entries) == 0 {
		b.WriteString("\nДобавьте данные, нажав «➕ Добавить данные»!")
		return b.String()
	}

	b.WriteString("\n")
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].GetSortOrder() != groups[j].GetSortOrder() {
			return groups[i].GetSortOrder() < groups[j].GetSortOrder()
		}
		return groups[i].GetName() < groups[j].GetName()
	})
	groupSet := make(map[string]*healthpb.CriterionGroup, len(groups))
	for _, g := range groups {
		groupSet[g.GetId()] = g
	}

	writeEntry := func(e *healthpb.UserCriterionEntry) {
		icon := statusIcon(e.GetStatus())
		b.WriteString(fmt.Sprintf("%s %s", icon, e.GetCriterionName()))
		if an := strings.TrimSpace(e.GetAnalysisName()); an != "" {
			b.WriteString(fmt.Sprintf(" (%s)", an))
		}
		if e.GetValue() != "" {
			b.WriteString(fmt.Sprintf(" — **%s**", e.GetValue()))
		}
		b.WriteString("\n")
	}

	for _, g := range groups {
		var block []*healthpb.UserCriterionEntry
		for _, e := range entries {
			if e.GetGroupId() == g.GetId() {
				block = append(block, e)
			}
		}
		if len(block) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("**%s**\n", g.GetName()))
		for _, e := range block {
			writeEntry(e)
		}
		b.WriteString("\n")
	}

	var ungrouped []*healthpb.UserCriterionEntry
	for _, e := range entries {
		gid := e.GetGroupId()
		if gid == "" || groupSet[gid] == nil {
			ungrouped = append(ungrouped, e)
		}
	}
	if len(ungrouped) > 0 {
		b.WriteString("**Прочее**\n")
		for _, e := range ungrouped {
			writeEntry(e)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
