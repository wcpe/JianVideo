package migration

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/settings"
)

type settingsPreflightReport struct {
	EstimatedRows int64
	Warnings      []string
	Blockers      []string
}

type legacySetting struct {
	Key   string
	Value string
}

func estimateSettingsPreflight(_ context.Context, db *gorm.DB) (StepPlan, error) {
	report, err := inspectSettingsPreflight(db)
	if err != nil {
		return StepPlan{}, err
	}
	return StepPlan{
		EstimatedRows: report.EstimatedRows,
		Warnings:      report.Warnings,
		Blockers:      report.Blockers,
	}, nil
}

func migrateSettingsPreflight(_ context.Context, _ *gorm.DB) error {
	return nil
}

func validateSettingsPreflight(_ context.Context, db *gorm.DB) (Validation, error) {
	report, err := inspectSettingsPreflight(db)
	if err != nil {
		return Validation{}, err
	}
	if len(report.Blockers) > 0 {
		return Validation{}, fmt.Errorf("旧 settings 预检存在阻塞项：%s", strings.Join(report.Blockers, "；"))
	}
	return Validation{
		Summary: fmt.Sprintf("旧 settings 预检通过：检查 %d 项，%d 个警告", report.EstimatedRows, len(report.Warnings)),
	}, nil
}

func inspectSettingsPreflight(db *gorm.DB) (settingsPreflightReport, error) {
	var report settingsPreflightReport
	if !tableExists(db, "settings") {
		return report, nil
	}

	var rows []legacySetting
	if err := db.Table("settings").Select("key", "value").Order("key ASC").Scan(&rows).Error; err != nil {
		return report, fmt.Errorf("读取旧 settings 失败: %w", err)
	}
	report.EstimatedRows = int64(len(rows))
	for _, row := range rows {
		known, err := settings.ValidateStored(row.Key, row.Value)
		switch {
		case err != nil:
			report.Blockers = append(report.Blockers, fmt.Sprintf("已登记 key %q 的值不符合 registry：%v", row.Key, err))
		case known:
		case isHighRiskSettingKeyName(row.Key):
			report.Blockers = append(report.Blockers, fmt.Sprintf("未登记高风险 key %q 可能影响启动或敏感配置边界，请先人工确认", row.Key))
		default:
			report.Warnings = append(report.Warnings, fmt.Sprintf("未登记历史 key %q 将原样保留，请确认业务不再依赖", row.Key))
		}
	}
	return report, nil
}

func isHighRiskSettingKeyName(key string) bool {
	words := settingKeyWords(key)
	if hasAnyWord(words, "jwt", "secret", "password", "passwd", "token", "credential", "credentials") {
		return true
	}
	if hasAnyWord(words, "database", "db", "sqlite") && hasAnyWord(words, "path", "file", "url", "dsn") {
		return true
	}
	if hasAnyWord(words, "api", "access", "private", "signing", "encryption", "master") && hasAnyWord(words, "key") {
		return true
	}
	return hasAnyWord(words, "listen", "bind", "server", "http", "https") && hasAnyWord(words, "port")
}

func settingKeyWords(key string) []string {
	runes := []rune(key)
	var normalized strings.Builder
	for index, current := range runes {
		var previous, next rune
		if index > 0 {
			previous = runes[index-1]
		}
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		camelBoundary := unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous))
		acronymBoundary := unicode.IsUpper(current) && unicode.IsUpper(previous) && unicode.IsLower(next)
		if camelBoundary || acronymBoundary {
			normalized.WriteByte(' ')
		}
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			normalized.WriteRune(unicode.ToLower(current))
		} else {
			normalized.WriteByte(' ')
		}
	}
	return strings.Fields(normalized.String())
}

func hasAnyWord(words []string, candidates ...string) bool {
	for _, word := range words {
		for _, candidate := range candidates {
			if word == candidate {
				return true
			}
		}
	}
	return false
}
