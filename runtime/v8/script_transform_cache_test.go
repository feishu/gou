package v8

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/application"
)

type walkCountingApp struct {
	application.Application
	walks int
}

func (app *walkCountingApp) Walk(path string, handler func(root, filename string, isdir bool) error, patterns ...string) error {
	app.walks++
	return app.Application.Walk(path, handler, patterns...)
}

func TestTransformTSSkipsSourceMapCacheOutsideDebug(t *testing.T) {
	option := option()
	option.Import = true
	option.SourceMap = true

	prepareTransformCacheTestApp(t, option)

	source, err := application.App.Read(filepath.Join("scripts", "app.ts"))
	if err != nil {
		t.Fatal(err)
	}

	transformed, err := TransformTS(filepath.Join("scripts", "app.ts"), source)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, transformed)
	assert.Empty(t, SourceMaps)
	assert.Empty(t, SourceCodes)
	assert.Empty(t, ModuleSourceMaps)

	imports := ImportMap[filepath.Join("scripts", "app.ts")]
	assert.Len(t, imports, 1)
	module, has := Modules[imports[0].AbsPath]
	assert.True(t, has)
	assert.NotEmpty(t, module.Source)
}

func TestTransformTSKeepsSourceMapCacheForDebugWithSourceMapEnabled(t *testing.T) {
	option := option()
	option.Import = true
	option.Debug = true
	option.SourceMap = true

	prepareTransformCacheTestApp(t, option)

	source, err := application.App.Read(filepath.Join("scripts", "app.ts"))
	if err != nil {
		t.Fatal(err)
	}

	transformed, err := TransformTS(filepath.Join("scripts", "app.ts"), source)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, transformed)
	assert.NotEmpty(t, SourceMaps[filepath.Join("scripts", "app.ts")])
	assert.NotEmpty(t, SourceCodes[filepath.Join("scripts", "app.ts")])
	assert.NotEmpty(t, ModuleSourceMaps)
}

func TestTransformTSSkipsSourceMapCacheInDebugWithoutSourceMap(t *testing.T) {
	option := option()
	option.Import = true
	option.Debug = true

	prepareTransformCacheTestApp(t, option)

	source, err := application.App.Read(filepath.Join("scripts", "app.ts"))
	if err != nil {
		t.Fatal(err)
	}

	transformed, err := TransformTS(filepath.Join("scripts", "app.ts"), source)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, transformed)
	assert.Empty(t, SourceMaps)
	assert.Empty(t, SourceCodes)
	assert.Empty(t, ModuleSourceMaps)
}

func TestTransformTSClonesSourceMapCacheForDebug(t *testing.T) {
	option := option()
	option.Import = true
	option.Debug = true
	option.SourceMap = true

	prepareTransformCacheTestApp(t, option)

	source, err := application.App.Read(filepath.Join("scripts", "app.ts"))
	if err != nil {
		t.Fatal(err)
	}

	transformed, err := TransformTS(filepath.Join("scripts", "app.ts"), source)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, transformed)
	assertClonedBytes(t, SourceMaps[filepath.Join("scripts", "app.ts")])
	assertClonedBytes(t, SourceCodes[filepath.Join("scripts", "app.ts")])
	for _, sourceMap := range ModuleSourceMaps {
		assertClonedBytes(t, sourceMap)
	}
}

func TestTransformTSAvoidsEmbeddingSharedModuleSourceInEveryImporter(t *testing.T) {
	option := option()
	option.Import = true
	option.SourceMap = true

	root := prepareSharedModuleTestApp(t, option)

	sharedPayload := strings.Repeat("shared-module-body-", 256)
	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "lib", "shared.ts"), `
globalThis.__sharedModulePayload = "`+sharedPayload+`";

export function sharedValue(prefix: string) {
  return prefix + globalThis.__sharedModulePayload;
}
`)

	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "first.ts"), `
import { sharedValue } from "./lib/shared";

export function Run() {
  return sharedValue("first");
}
`)

	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "second.ts"), `
import { sharedValue } from "./lib/shared";

export function Run() {
  return sharedValue("second");
}
`)

	first, err := application.App.Read(filepath.Join("scripts", "first.ts"))
	if err != nil {
		t.Fatal(err)
	}
	firstSource, err := TransformTS(filepath.Join("scripts", "first.ts"), first)
	if err != nil {
		t.Fatal(err)
	}

	second, err := application.App.Read(filepath.Join("scripts", "second.ts"))
	if err != nil {
		t.Fatal(err)
	}
	secondSource, err := TransformTS(filepath.Join("scripts", "second.ts"), second)
	if err != nil {
		t.Fatal(err)
	}

	firstText := string(firstSource)
	secondText := string(secondSource)
	assert.Equal(t, 0, strings.Count(firstText, "shared-module-body"))
	assert.Equal(t, 0, strings.Count(secondText, "shared-module-body"))
	assert.Less(t, len(firstText), len(sharedPayload)/4)
	assert.Less(t, len(secondText), len(sharedPayload)/4)

	imports := ImportMap[filepath.Join("scripts", "first.ts")]
	assert.Len(t, imports, 1)
	module, has := Modules[imports[0].AbsPath]
	assert.True(t, has)
	assert.Greater(t, strings.Count(module.Source, "shared-module-body"), 0)
}

func TestRuntimeScriptSourceExecutesImportedModuleWithoutEmbeddingInScriptSource(t *testing.T) {
	option := option()
	option.Import = true
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 1
	option.HeapSizeLimit = 4294967296
	option.SourceMap = true

	root := prepareSharedModuleRuntimeTestApp(t, option)

	sharedPayload := strings.Repeat("shared-module-body-", 128)
	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "lib", "shared.ts"), `
globalThis.__sharedModulePayload = "`+sharedPayload+`";

export function sharedValue(prefix: string) {
  return prefix + globalThis.__sharedModulePayload;
}

export const label = "module-label";
`)

	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "consumer.ts"), `
import * as shared from "./lib/shared";
import { sharedValue, label as sharedLabel } from "./lib/shared";

export function Run() {
  return [sharedValue("direct-"), shared.sharedValue("namespace-"), sharedLabel];
}
`)

	source, err := application.App.Read(filepath.Join("scripts", "consumer.ts"))
	if err != nil {
		t.Fatal(err)
	}
	script, err := MakeScript(source, filepath.Join("scripts", "consumer.ts"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(script.Source, "shared-module-body") {
		t.Fatal("script source should not embed shared module body")
	}

	v8ctx, err := script.NewContext("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v8ctx.Close()

	res, err := v8ctx.Call("Run")
	if err != nil {
		t.Fatal(err)
	}

	data, ok := res.([]interface{})
	if !ok {
		t.Fatalf("expected array result, got %T", res)
	}
	assert.Equal(t, "direct-"+sharedPayload, data[0])
	assert.Equal(t, "namespace-"+sharedPayload, data[1])
	assert.Equal(t, "module-label", data[2])
}

func TestCloneBytesCopiesAndTrimsBackingArray(t *testing.T) {
	source := make([]byte, 4096)
	copy(source, []byte("0123456789abcdef"))
	data := source[:16]

	cloned := cloneBytes(data)

	assert.Equal(t, data, cloned)
	assert.Equal(t, len(cloned), cap(cloned))

	source[0] = 'x'
	assert.Equal(t, byte('0'), cloned[0])

	cloned[1] = 'y'
	assert.Equal(t, byte('1'), source[1])
}

func TestTSConfigGetFileNameCachesResolvedPath(t *testing.T) {
	root := t.TempDir()
	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "runtime", "ts", "lib", "foo.ts"), `export const foo = "ok";`)

	baseApp, err := application.OpenFromDisk(root)
	if err != nil {
		t.Fatal(err)
	}

	oldApp := application.App
	app := &walkCountingApp{Application: baseApp}
	application.Load(app)
	t.Cleanup(func() { application.App = oldApp })

	tsconfig := &TSConfig{
		CompilerOptions: &TSConfigCompilerOptions{
			Paths: map[string][]string{
				"@lib/*": {"./scripts/runtime/ts/lib/*"},
			},
		},
	}

	file, match, err := tsconfig.GetFileName("@lib/foo")
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, match)
	assert.Equal(t, filepath.Join("scripts", "runtime", "ts", "lib", "foo.ts"), file)

	file, match, err = tsconfig.GetFileName("@lib/foo")
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, match)
	assert.Equal(t, filepath.Join("scripts", "runtime", "ts", "lib", "foo.ts"), file)
	assert.Equal(t, 1, app.walks)
}

func assertClonedBytes(t *testing.T, data []byte) {
	t.Helper()

	if len(data) == 0 {
		t.Fatal("expected cached bytes")
	}
	if cap(data) != len(data) {
		t.Fatalf("expected cloned bytes with cap equal len, got len %d cap %d", len(data), cap(data))
	}
}

func prepareSharedModuleRuntimeTestApp(t *testing.T, option *Option) string {
	t.Helper()

	root := prepareSharedModuleTestApp(t, option)
	if err := Start(option); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Stop)

	return root
}

func prepareSharedModuleTestApp(t *testing.T, option *Option) string {
	t.Helper()

	root := t.TempDir()
	app, err := application.OpenFromDisk(root)
	if err != nil {
		t.Fatal(err)
	}

	oldApp := application.App
	oldRuntimeOption := runtimeOption
	oldModules := Modules
	oldImportMap := ImportMap
	oldSourceMaps := SourceMaps
	oldSourceCodes := SourceCodes
	oldModuleSourceMaps := ModuleSourceMaps

	application.Load(app)
	runtimeOption = option
	CLearModules()

	t.Cleanup(func() {
		application.App = oldApp
		runtimeOption = oldRuntimeOption
		Modules = oldModules
		ImportMap = oldImportMap
		SourceMaps = oldSourceMaps
		SourceCodes = oldSourceCodes
		ModuleSourceMaps = oldModuleSourceMaps
	})

	return root
}

func prepareTransformCacheTestApp(t *testing.T, option *Option) {
	t.Helper()

	root := t.TempDir()
	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "app.ts"), `
import { helper } from "./lib/helper";

export function run() {
  return helper();
}
`)
	writeTransformCacheTestFile(t, root, filepath.Join("scripts", "lib", "helper.ts"), `
export function helper() {
  return "ok";
}
`)

	app, err := application.OpenFromDisk(root)
	if err != nil {
		t.Fatal(err)
	}

	oldApp := application.App
	oldRuntimeOption := runtimeOption
	oldModules := Modules
	oldImportMap := ImportMap
	oldSourceMaps := SourceMaps
	oldSourceCodes := SourceCodes
	oldModuleSourceMaps := ModuleSourceMaps

	application.Load(app)
	runtimeOption = option
	CLearModules()

	t.Cleanup(func() {
		application.App = oldApp
		runtimeOption = oldRuntimeOption
		Modules = oldModules
		ImportMap = oldImportMap
		SourceMaps = oldSourceMaps
		SourceCodes = oldSourceCodes
		ModuleSourceMaps = oldModuleSourceMaps
	})
}

func writeTransformCacheTestFile(t *testing.T, root string, name string, content string) {
	t.Helper()

	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
