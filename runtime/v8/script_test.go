package v8

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/process"
)

// func TestLoad(t *testing.T) {
// 	prepare(t)
// 	time.Sleep(20 * time.Millisecond)
// 	assert.Equal(t, 3, len(Scripts))
// 	assert.Equal(t, 1, len(RootScripts))
// 	assert.Equal(t, 2, len(chIsoReady))
// }

func TestTransformTS(t *testing.T) {

	option := option()
	option.Mode = "standard"
	option.Import = true
	option.HeapSizeLimit = 4294967296
	prepare(t, option)
	defer Stop()

	files := map[string]string{
		"app.ts":       filepath.Join("scripts", "runtime", "ts", "app.ts"),
		"lib.hello.ts": filepath.Join("scripts", "runtime", "ts", "lib", "hello.ts"),
	}

	app, err := application.App.Read(files["app.ts"])
	if err != nil {
		t.Fatal(err)
	}

	appSource, err := TransformTS(files["app.ts"], app)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, appSource)
	imports := ImportMap[files["app.ts"]]
	assert.Len(t, imports, 3)
	for _, im := range imports {
		module, has := Modules[im.AbsPath]
		assert.True(t, has)
		assert.NotEmpty(t, module.Source)
	}
}

func TestTransformTSWithTSConfig(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.Import = true
	option.HeapSizeLimit = 4294967296

	// add tsconfig
	tsconfig := &TSConfig{
		CompilerOptions: &TSConfigCompilerOptions{
			Paths: map[string][]string{
				"@yao/*": {"./scripts/.types/*"},
				"@lib/*": {"./scripts/runtime/ts/lib/*"},
			},
		},
	}
	option.TSConfig = tsconfig

	prepare(t, option)
	defer Stop()

	files := map[string]string{
		"page.ts":      filepath.Join("scripts", "runtime", "ts", "page.ts"),
		"lib.hello.ts": filepath.Join("scripts", "runtime", "ts", "lib", "hello.ts"),
	}

	page, err := application.App.Read(files["page.ts"])
	if err != nil {
		t.Fatal(err)
	}

	pageSource, err := TransformTS(files["page.ts"], page)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, pageSource)
	imports := ImportMap[files["page.ts"]]
	assert.Len(t, imports, 2)
	for _, im := range imports {
		module, has := Modules[im.AbsPath]
		assert.True(t, has)
		assert.NotEmpty(t, module.Source)
	}
}

func TestExecStandard(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.Import = true
	option.HeapSizeLimit = 4294967296
	prepare(t, option)
	defer Stop()

	Load(filepath.Join("scripts", "runtime", "ts", "app.ts"), "runtime.ts.app")
	script, err := Select("runtime.ts.app")
	if err != nil {
		t.Fatal(err)
	}

	p := process.New("scripts.runtime.ts.app.FooBar")
	res := script.Exec(p)
	data, ok := res.([]interface{})
	if !ok {
		t.Fatal("result error")
	}

	assert.Len(t, data, 3)
	assert.Contains(t, data[0], "Hello")
}

func TestExecStandardUsesDispatcherPool(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 2
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	script := &Script{
		ID:   "exec-standard-pool-test",
		File: "exec-standard-pool-test.js",
		Source: `
function Echo(value) {
	return [value, __yao_data.SID, __yao_data.DATA.name]
}
`,
	}
	p := process.New("scripts.exec-standard-pool-test.Echo", "hello").WithSID("sid-123").WithGlobal(map[string]interface{}{"name": "alice"})

	res := script.Exec(p)
	data, ok := res.([]interface{})
	if !ok {
		t.Fatalf("expected array result, got %T", res)
	}
	if len(data) != 3 || data[0] != "hello" || data[1] != "sid-123" || data[2] != "alice" {
		t.Fatalf("unexpected exec result: %#v", data)
	}
	if dispatcher == nil {
		t.Fatal("dispatcher should be initialized in standard mode")
	}
	stats := dispatcher.Stats()
	if stats.Created == 0 || stats.Active == 0 {
		t.Fatalf("expected standard exec to use dispatcher pool, got %+v", stats)
	}
}

func TestExecPerformanceMatchesInlineStandard(t *testing.T) {
	source := `
function Echo(value) {
	return [value, __yao_data.SID, __yao_data.DATA.name]
}
`

	standardOption := option()
	standardOption.Mode = "standard"
	standardOption.MinSize = 1
	standardOption.MaxSize = 2
	standardOption.HeapSizeLimit = 4294967296
	prepareSetup(t, standardOption)

	standard := (&Script{ID: "exec-inline-standard-test", File: "exec-inline-standard-test.js", Source: source}).Exec(
		process.New("scripts.exec-inline-standard-test.Echo", "hello").WithSID("sid-123").WithGlobal(map[string]interface{}{"name": "alice"}),
	)
	cleanupDispatcherForTest(t)

	performanceOption := option()
	performanceOption.Mode = "performance"
	performanceOption.MinSize = 1
	performanceOption.MaxSize = 2
	performanceOption.HeapSizeLimit = 4294967296
	prepareSetup(t, performanceOption)
	defer cleanupDispatcherForTest(t)

	performance := (&Script{ID: "exec-inline-performance-test", File: "exec-inline-performance-test.js", Source: source}).Exec(
		process.New("scripts.exec-inline-performance-test.Echo", "hello").WithSID("sid-123").WithGlobal(map[string]interface{}{"name": "alice"}),
	)

	assert.Equal(t, standard, performance)
}
