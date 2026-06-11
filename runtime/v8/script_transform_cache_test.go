package v8

import (
	"os"
	"path/filepath"
	"testing"

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

func TestTransformTSKeepsSourceMapCacheForDebug(t *testing.T) {
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
	assert.NotEmpty(t, SourceMaps[filepath.Join("scripts", "app.ts")])
	assert.NotEmpty(t, SourceCodes[filepath.Join("scripts", "app.ts")])
	assert.NotEmpty(t, ModuleSourceMaps)
}

func TestTransformTSClonesSourceMapCacheForDebug(t *testing.T) {
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
	assertClonedBytes(t, SourceMaps[filepath.Join("scripts", "app.ts")])
	assertClonedBytes(t, SourceCodes[filepath.Join("scripts", "app.ts")])
	for _, sourceMap := range ModuleSourceMaps {
		assertClonedBytes(t, sourceMap)
	}
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
