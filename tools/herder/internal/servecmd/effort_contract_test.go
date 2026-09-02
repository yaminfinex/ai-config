package servecmd

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

func TestEffortAllowListsMatchFleetAndWeb(t *testing.T) {
	fleetSource := readContractSource(t, filepath.Join("..", "..", "..", "fleet", "spawn.sh"))
	webSource := readContractSource(t, filepath.Join("..", "..", "web", "src", "features", "launch", "launchModel.ts"))

	fleet := make(map[string][]string)
	for _, match := range regexp.MustCompile(`\b(claude|codex):([a-z]+)\b`).FindAllStringSubmatch(fleetSource, -1) {
		fleet[match[1]] = append(fleet[match[1]], match[2])
	}

	effortsBlock := regexp.MustCompile(`(?s)const efforts: Record<LaunchTool, string\[\]> = \{(.*?)\n\}`).FindStringSubmatch(webSource)
	if len(effortsBlock) != 2 {
		t.Fatal("launchModel.ts must contain one parseable efforts map")
	}
	web := make(map[string][]string)
	for _, entry := range regexp.MustCompile(`(?m)^\s*(claude|codex): \[([^\]]+)\],$`).FindAllStringSubmatch(effortsBlock[1], -1) {
		for _, value := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(entry[2], -1) {
			web[entry[1]] = append(web[entry[1]], value[1])
		}
	}

	if !reflect.DeepEqual(fleet, effortLevelsByTool) {
		t.Fatalf("fleet effort allow-lists=%v server=%v", fleet, effortLevelsByTool)
	}
	if !reflect.DeepEqual(web, effortLevelsByTool) {
		t.Fatalf("web effort allow-lists=%v server=%v", web, effortLevelsByTool)
	}
}

func readContractSource(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
