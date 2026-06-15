package v8

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

// Scripts loaded scripts
var Scripts = map[string]*Script{}

// Modules the scripts for modules
var Modules = map[string]Module{}

// ImportMap the import maps
var ImportMap = map[string][]Import{}

// RootScripts the scripts for studio
var RootScripts = map[string]*Script{}

// var importRe = regexp.MustCompile(`import\s*\t*\n*[^;]*;`)
var importRe = regexp.MustCompile(`import\s+\t*\n*(\*\s+as\s+\w+|\{[^}]+\}|\w+)\s+from\s+["']([^"']+)["'];?`)
var exportRe = regexp.MustCompile(`export\s+(default|function|class|const|var|let)\s+`)

var internalKeepModuleSuffixes = []string{"/yao.ts", "/yao", "/gou", "/gou.ts"}
var internalKeepModules = []string{"@yao", "@yaoapps", "@yaoapp", "@gou"}

// the lock for the scripts
var syncLock = sync.Mutex{}

// GetModuleName get the module name
func GetModuleName(file string) string {
	replaces := []string{"@", "/", ".", "-", "[", "]", "(", ")", "{", "}", ":", ",", ";", " ", "\t", "\n", "\r"}
	for _, replace := range replaces {
		file = strings.ReplaceAll(file, replace, "_")
	}
	return file
}

// NewScript create a new script
func NewScript(file string, id string, timeout ...time.Duration) *Script {

	t := time.Duration(0)
	if len(timeout) > 0 {
		t = timeout[0]
	}

	return &Script{
		ID:      id,
		File:    file,
		Timeout: t,
	}
}

// Open open the script
func (script *Script) Open(source []byte) error {
	var err error = nil
	if strings.HasSuffix(script.File, ".ts") {
		source, err = TransformTS(script.File, source)
		if err != nil {
			return err
		}
	}
	script.Source = string(source)
	return nil
}

// MakeScript make a script from source
func MakeScript(source []byte, file string, timeout time.Duration, isroot ...bool) (*Script, error) {
	syncLock.Lock()
	defer syncLock.Unlock()
	script := NewScript(file, file, timeout)
	err := script.Open(source)
	if err != nil {
		return nil, err
	}
	script.Root = false
	if len(isroot) > 0 {
		script.Root = isroot[0]
	}
	return script, nil
}

// Exists check if the script exists
func Exists(id string) bool {
	if strings.HasPrefix(id, "scripts.") {
		id = strings.Replace(id, "scripts.", "", 1)
	}
	_, has := Scripts[id]
	return has
}

// Load load the script
func Load(file string, id string) (*Script, error) {
	source, err := application.App.Read(file)
	if err != nil {
		return nil, err
	}
	script, err := MakeScript(source, file, 5*time.Second, false)
	if err != nil {
		return nil, err
	}
	syncLock.Lock()
	Scripts[id] = script
	syncLock.Unlock()
	return script, nil
}

// LoadRoot load the script with root privileges
func LoadRoot(file string, id string) (*Script, error) {
	source, err := application.App.Read(file)
	if err != nil {
		return nil, err
	}
	script, err := MakeScript(source, file, 5*time.Second, true)
	if err != nil {
		return nil, err
	}
	syncLock.Lock()
	RootScripts[id] = script
	syncLock.Unlock()
	return script, nil
}

// CLearModules clear the modules cache
func CLearModules() {
	Modules = map[string]Module{}
	ImportMap = map[string][]Import{}
	clearSourceMaps()
	if runtimeOption.TSConfig != nil {
		runtimeOption.TSConfig.clearCache()
	}
}

// TransformTS transform the typescript
func TransformTS(file string, source []byte) ([]byte, error) {

	tsCode, err := tsImports(file, removeCommentsAndKeepLines(source))
	if err != nil {
		return nil, err
	}

	keepSourceMap := shouldKeepSourceMap()
	result := api.Transform(tsCode, api.TransformOptions{
		Loader:     api.LoaderTS,
		Target:     api.ESNext,
		Sourcefile: file,
		Sourcemap:  sourceMapOption(keepSourceMap),
	})

	if len(result.Errors) > 0 {
		return nil, newTransformErrorFromMessages(file, result.Errors)
	}

	if keepSourceMap {
		SourceMaps[file] = cloneBytes(result.Map)
		SourceCodes[file] = cloneBytes(result.Code)
	}

	jsCode := result.Code

	return []byte(
		exportRe.ReplaceAllStringFunc(string(jsCode), func(m string) string {
			return strings.ReplaceAll(m, "export ", "")
		})), nil
}

func runtimeScriptSource(script *Script) string {
	if script == nil {
		return ""
	}

	source := script.Source
	if !runtimeOption.Import {
		return source
	}

	importCodes := runtimeImportCodes(script.File)
	if len(importCodes) == 0 {
		return source
	}
	return strings.Join(importCodes, ";") + source
}

func runtimeImportCodes(file string) []string {
	importCodes := []string{}
	loaded := map[string]bool{}
	if imports, has := ImportMap[file]; has {
		for _, imp := range imports {
			module, has := Modules[imp.AbsPath]
			if has {
				if !loaded[imp.AbsPath] {
					importCodes = append(importCodes, module.Source)
					loaded[imp.AbsPath] = true
				}
				importCodes = append(importCodes, importAliasCode(imp, module))
			}
		}
	}
	return importCodes
}

func importAliasCode(imp Import, module Module) string {
	return fmt.Sprintf("const %s = %s;", imp.Name, module.GlobalName)
}

func shouldKeepSourceMap() bool {
	return runtimeOption.Debug && runtimeOption.SourceMap
}

func sourceMapOption(keep bool) api.SourceMap {
	if keep {
		return api.SourceMapExternal
	}
	return api.SourceMapNone
}

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}
	cloned := make([]byte, len(data))
	copy(cloned, data)
	return cloned
}

type entry struct {
	absfile string
	source  string
	file    string
}

func removeCommentsAndKeepLines(code []byte) []byte {
	lines := strings.Split(string(code), "\n")
	for i, line := range lines {
		// Start with /*
		if strings.HasPrefix(strings.TrimSpace(line), "/*") {
			lines[i] = ""
			for {
				if strings.Contains(line, "*/") {
					break
				}
				i++
				line = lines[i]
				lines[i] = ""
			}
		}

		// Start with //
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func getEntryPoints(file string, tsCode string, loaded map[string]bool) (string, []entry, error) {
	entryPoints := []entry{}
	root := application.App.Root()
	absFile := filepath.Join(root, file)

	tsCode, imports, err := replaceImportCode(file, []byte(tsCode))
	if err != nil {
		return "", nil, err
	}
	ImportMap[file] = imports
	entryPoints = append(entryPoints, entry{file: file, absfile: absFile, source: tsCode})

	for _, imp := range imports {
		if loaded[imp.Path] {
			continue
		}
		loaded[imp.Path] = true
		source, err := application.App.Read(imp.Path)
		if err != nil {
			return "", nil, err
		}

		_, subEntryPoints, err := getEntryPoints(imp.Path, string(source), loaded)
		if err != nil {
			return "", nil, err
		}

		entryPoints = append(entryPoints, subEntryPoints...)
	}
	return tsCode, entryPoints, nil

}

func loadModule(file string, tsCode string) error {

	_, imports, err := replaceImportCode(file, []byte(tsCode))
	if err != nil {
		return err
	}
	ImportMap[file] = imports

	for _, imp := range imports {
		if _, has := Modules[imp.AbsPath]; has {
			continue
		}

		source, err := application.App.Read(imp.Path)
		if err != nil {
			return err
		}
		if err := buildModule(imp.Path, string(source)); err != nil {
			return err
		}
	}

	return nil
}

func buildModule(file string, tsCode string) error {
	root := application.App.Root()
	absFile := filepath.Join(root, file)

	if _, has := Modules[absFile]; has {
		return nil
	}

	globalName := GetModuleName(file)
	loaded := map[string]bool{file: true}
	_, entryPoints, err := getEntryPoints(file, tsCode, loaded)
	if err != nil {
		return err
	}

	codes := map[string]string{}
	for _, entry := range entryPoints {
		cacheBuildSource(codes, entry.absfile, entry.source)
	}

	keepSourceMap := shouldKeepSourceMap()
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{absFile},
		Bundle:      true,
		Write:       false,
		Target:      api.ESNext,
		Format:      api.FormatIIFE,
		GlobalName:  globalName,
		Loader: map[string]api.Loader{
			".ts": api.LoaderTS,
		},
		Sourcemap: sourceMapOption(keepSourceMap),
		Outdir:    "/outdir",
		Plugins: []api.Plugin{
			{
				Name: "custom-import-plugin",
				Setup: func(build api.PluginBuild) {
					build.OnLoad(api.OnLoadOptions{Filter: `.*\.ts$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
						contents := codes[filepath.Clean(args.Path)]
						return api.OnLoadResult{
							Contents: &contents,
							Loader:   api.LoaderTS,
						}, nil
					})
				},
			},
		},
	})

	if len(result.Errors) > 0 {
		return newTransformErrorFromMessages(file, result.Errors)
	}

	for _, out := range result.OutputFiles {
		if strings.HasSuffix(out.Path, ".js.map") {
			if !keepSourceMap {
				continue
			}
			ModuleSourceMaps[absFile] = cloneBytes(out.Contents)

		} else if strings.HasSuffix(out.Path, ".js") {
			Modules[absFile] = Module{
				File:       file,
				GlobalName: globalName,
				Source:     string(out.Contents),
			}
		}
	}

	return nil
}

func cacheBuildSource(codes map[string]string, file string, source string) {
	codes[filepath.Clean(file)] = source
	if realFile, err := filepath.EvalSymlinks(file); err == nil {
		codes[filepath.Clean(realFile)] = source
	}
}

func tsImports(file string, source []byte) (string, error) {

	err := loadModule(file, string(source))
	if err != nil {
		return "", err
	}

	tsCode := importRe.ReplaceAllStringFunc(string(source), func(m string) string { // Remove the import as comments
		lines := strings.Split(m, "\n")
		for i, line := range lines {
			lines[i] = "// " + line
		}
		return strings.Join(lines, "\n")
	})

	return tsCode, nil
}

func replaceImportCode(file string, source []byte) (string, []Import, error) {
	var err error = nil
	errors := []string{}
	imports := []Import{}
	tsCode := importRe.ReplaceAllStringFunc(string(source), func(m string) string {
		matches := importRe.FindStringSubmatch(m)
		if len(matches) == 3 {
			importClause, importPath := matches[1], matches[2]

			// Filter the internal keep modules
			for _, keep := range internalKeepModuleSuffixes {
				if strings.HasSuffix(importPath, keep) {
					lines := strings.Split(m, "\n")
					for i, line := range lines {
						lines[i] = "// " + line

					}
					return strings.Join(lines, "\n")
				}
			}
			for _, keep := range internalKeepModules {
				if strings.HasPrefix(importPath, keep) {
					lines := strings.Split(m, "\n")
					for i, line := range lines {
						lines[i] = "// " + line
					}
					return strings.Join(lines, "\n")
				}
			}

			relImportPath, err := getImportPath(file, importPath)
			if err != nil {
				errors = append(errors, err.Error())
				return m
			}

			absImportPath := filepath.Join(application.App.Root(), relImportPath)

			name := strings.TrimSpace(importClause)
			if strings.Index(importClause, "*") >= 0 {
				arr := strings.Split(importClause, " as ")
				if len(arr) == 2 {
					name = strings.TrimSpace(arr[1])
				}
			} else if strings.Index(importClause, " as ") >= 0 {
				name = strings.ReplaceAll(importClause, " as ", ":")
			}

			imports = append(imports, Import{
				Name:    name,
				Path:    relImportPath,
				AbsPath: absImportPath,
				Clause:  importClause,
			})
			return fmt.Sprintf(`import %s from "%s";`, importClause, absImportPath)
		}
		return m
	})

	if len(errors) > 0 {
		err = newTransformErrorFromTexts(file, errors)
	}

	return tsCode, imports, err
}

func getImportPath(file string, path string) (string, error) {

	var tsfile string
	var fromTsConfig bool = false
	if runtimeOption.TSConfig != nil {
		var err error
		tsfile, fromTsConfig, err = runtimeOption.TSConfig.GetFileName(path)
		if err != nil {
			return "", err
		}
		if fromTsConfig {
			file = tsfile
		}
	}

	if !fromTsConfig {
		relpath := filepath.Dir(file)
		file = filepath.Join(relpath, path)
	}

	if !strings.HasSuffix(path, ".ts") {
		if exist, _ := application.App.Exists(file + ".ts"); exist {
			file = file + ".ts"
			return file, nil

		} else if exist, _ := application.App.Exists(filepath.Join(path, "index.ts")); exist {
			file = file + "index.ts"
			return file, nil
		}
	}

	if exist, _ := application.App.Exists(file); !exist {
		return "", fmt.Errorf("file %s not exists", file)
	}

	return file, nil
}

func newTransformErrorFromMessages(defaultFile string, messages []api.Message) error {
	items := make([]TransformErrorItem, 0, len(messages))
	for _, message := range messages {
		items = append(items, transformErrorItemFromMessage(defaultFile, message))
	}
	if len(items) == 0 {
		return nil
	}
	return &TransformError{Items: items}
}

func newTransformErrorFromTexts(defaultFile string, texts []string) error {
	items := make([]TransformErrorItem, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		items = append(items, TransformErrorItem{
			File: normalizeTransformErrorFile(defaultFile, ""),
			Text: text,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return &TransformError{Items: items}
}

func transformErrorItemFromMessage(defaultFile string, message api.Message) TransformErrorItem {
	item := TransformErrorItem{
		File: normalizeTransformErrorFile(defaultFile, ""),
		Text: message.Text,
	}

	if message.Location != nil {
		item.File = normalizeTransformErrorFile(defaultFile, message.Location.File)
		item.Line = message.Location.Line
		item.Column = message.Location.Column + 1
	}

	return item
}

func normalizeTransformErrorFile(defaultFile string, filename string) string {
	if filename == "" {
		filename = defaultFile
	}

	if application.App != nil {
		root := application.App.Root()
		if root != "" {
			if rel, err := filepath.Rel(root, filename); err == nil && !strings.HasPrefix(rel, "..") {
				filename = rel
			}
		}
	}

	return filepath.ToSlash(filename)
}

// Transform the javascript
func Transform(source string, globalName string) string {
	result := api.Transform(source, api.TransformOptions{
		Loader:     api.LoaderJS,
		Format:     api.FormatIIFE,
		GlobalName: globalName,
	})
	return string(result.Code)
}

// Select a script
func Select(id string) (*Script, error) {
	script, has := Scripts[id]
	if !has {
		return nil, fmt.Errorf("script %s not exists", id)
	}
	return script, nil
}

// SelectRoot a script with root privileges
func SelectRoot(id string) (*Script, error) {

	script, has := RootScripts[id]
	if has {
		return script, nil
	}

	script, has = Scripts[id]
	if !has {
		return nil, fmt.Errorf("script(root) %s not exists", id)
	}

	return script, nil
}

// NewContext create a new context
func (script *Script) NewContext(sid string, global map[string]interface{}) (*Context, error) {

	timeout := script.Timeout
	if timeout == 0 {
		timeout = time.Duration(runtimeOption.ContextTimeout) * time.Millisecond
	}

	runner, err := dispatcher.Select(time.Duration(runtimeOption.DefaultTimeout) * time.Millisecond)
	if err != nil {
		return nil, err
	}
	ctx, err := runner.Context()
	if err != nil {
		runner.Destroy(nil)
		return nil, err
	}

	return &Context{
		ID:          script.ID,
		Sid:         sid,
		Data:        global,
		Root:        script.Root,
		Timeout:     timeout,
		script:      script,
		Runner:      runner,
		Context:     ctx,
		SourceRoots: script.SourceRoots,
	}, nil
}

// Exec execute the script
// the default mode is "standard" and the other value is "performance".
// the "standard" mode save memory but will run slower. can be used in most cases, especially in arm64 device.
// the "performance" mode need more memory but will run faster. can be used in high concurrency and large script.
func (script *Script) Exec(process *process.Process) interface{} {
	return script.execPool(process)
}

func (script *Script) execPool(process *process.Process) interface{} {
	runner, err := dispatcher.Select(time.Duration(runtimeOption.DefaultTimeout) * time.Millisecond)
	if err != nil {
		exception.New("scripts.%s.%s %s", 500, script.ID, process.Method, err.Error()).Throw()
		return nil
	}

	return runner.ExecInvocation(runnerInvocation{
		script: script,
		method: process.Method,
		args:   process.Args,
		sid:    process.Sid,
		global: process.Global,
	})
}

// ContextTimeout get the context timeout
func (script *Script) ContextTimeout() time.Duration {
	if script.Timeout > 0 {
		return script.Timeout
	}
	return time.Duration(runtimeOption.ContextTimeout) * time.Millisecond
}
