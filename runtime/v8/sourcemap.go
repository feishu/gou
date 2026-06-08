package v8

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/go-sourcemap/sourcemap"
	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
	"rogchap.com/v8go"
)

// SourceMaps the source maps
var SourceMaps = map[string][]byte{}

// SourceCodes the source codes
var SourceCodes = map[string][]byte{}

// ModuleSourceMaps the source maps for modules
var ModuleSourceMaps = map[string][]byte{}

// reStackEntry the stack entry regex
var reStackEntry = regexp.MustCompile(`at[ ]+(?P<Function>[^(]+)[ ]+\((?P<File>[^:]+):(?P<Line>\d+):(?P<Column>\d+)\)`)

// SourceMap source map
type SourceMap struct {
	Version        int      `json:"version"`
	File           string   `json:"file"`
	SourceRoot     string   `json:"sourceRoot,omitempty"`
	Sources        []string `json:"sources"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
	SourcesContent []string `json:"sourcesContent,omitempty"`
	bytes          []byte
	path           string
	offset         int
	count          int
}

// StackLogEntry stack log entry
type StackLogEntry struct {
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Message  string `json:"message,omitempty"`
}

// StackLogEntryList stack log entry list
type StackLogEntryList []*StackLogEntry

// sourceMapIndex the source map index
type sourceMapIndex struct {
	indexes []int
	maps    []*SourceMap
	file    string
}

type debugIndexedSourceMap struct {
	Version  int                     `json:"version"`
	File     string                  `json:"file,omitempty"`
	Sections []debugSourceMapSection `json:"sections"`
}

type debugSourceMapSection struct {
	Offset debugSourceMapOffset `json:"offset"`
	Map    *SourceMap           `json:"map"`
}

type debugSourceMapOffset struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func clearSourceMaps() {
	ModuleSourceMaps = map[string][]byte{}
	SourceMaps = map[string][]byte{}
	SourceCodes = map[string][]byte{}
}

// PrintException print the exception
func PrintException(method string, args []interface{}, jserr *v8go.JSError, rootMapping interface{}) {

	if runtimeOption.Debug {
		ex := exception.New(jserr.Message, 500)
		color.Red("\n----------------------------------")
		color.Red("Script Exception: %s", fmt.Sprintf("%d %s", ex.Code, ex.Message))
		color.Red("----------------------------------")

		color.Red("%s\n", StackTrace(jserr, rootMapping))
		fmt.Println(color.YellowString("\nMethod:"), color.WhiteString("%s", method))
		color.Yellow("Args:")
		raw, _ := jsoniter.MarshalToString(args)
		color.White("%s\n", raw)
	}
}

// StackTrace get the stack trace
func StackTrace(jserr *v8go.JSError, rootMapping interface{}) string {

	ex := exception.New(jserr.Message, 500)

	// Development mode will show the stack trace
	entries := parseStackTrace(jserr.StackTrace)
	if entries == nil || len(entries) == 0 {
		return jserr.StackTrace
	}

	output, err := entries.String(rootMapping)
	if err != nil {
		return err.Error() + "\n" + jserr.StackTrace
	}

	return fmt.Sprintf("%s\n%s", fmt.Sprintf("%d %s", ex.Code, ex.Message), output)
}

func (entry *StackLogEntry) String() string {
	return fmt.Sprintf("    at %s (%s:%d:%d)", entry.Function, entry.File, entry.Line, entry.Column)
}

// String the stack log entry list to string
func (list StackLogEntryList) String(rootMapping interface{}) (string, error) {
	if len(list) == 0 {
		return "", fmt.Errorf("StackLogEntryList.String(), empty list")
	}

	index, err := parseSourceMaps(list[0].File)
	if err != nil || index == nil {
		return "", fmt.Errorf("StackLogEntryList.String(), parse source maps error %s", err)
	}

	for _, entry := range list {
		line := entry.Line
		sm := index.getSourceMap(line)
		line -= sm.offset
		smap, err := sourcemap.Parse(sm.path, sm.bytes)
		if err != nil {
			return "", fmt.Errorf("StackLogEntryList.String(), parse source maps error. %s", err)
		}

		file, fn, line, col, ok := smap.Source(line, entry.Column)
		if ok {
			entry.File = fmtFilePath(file, rootMapping)
			entry.Line = line
			entry.Column = col
			if fn != "" {
				entry.Function = fn
			}
		}
	}

	output := []string{}
	for _, entry := range list {
		output = append(output, entry.String())
	}
	return strings.Join(output, "\n"), nil
}

func (index *sourceMapIndex) getSourceMap(line int) *SourceMap {
	for i, offset := range index.indexes {
		if line < offset {
			return index.maps[i]
		}
	}
	return index.maps[0]
}

func parseSourceMaps(file string) (*sourceMapIndex, error) {

	data, has := SourceMaps[file]
	if !has {
		return nil, nil
	}

	source, has := SourceCodes[file]
	if !has {
		return nil, nil
	}

	sm, err := NewSourceMap(data)
	if err != nil {
		return nil, err
	}

	sm.count = cntSource(string(source))

	index := &sourceMapIndex{
		indexes: []int{},
		maps:    []*SourceMap{sm},
		file:    file,
	}
	var offset int = 0
	if runtimeOption.Import {
		if imports, has := ImportMap[file]; has {
			for _, imp := range imports {
				data := ModuleSourceMaps[imp.AbsPath]
				if !has {
					continue
				}

				module, has := Modules[imp.AbsPath]
				if !has {
					continue
				}

				ism, err := NewSourceMap(data)
				if err != nil {
					return nil, err
				}
				ism.count = cntSource(module.Source)
				index.maps = append(index.maps, ism)
				index.indexes = append(index.indexes, offset)
				ism.offset = offset
				ism.path = imp.Path
				offset += ism.count
			}
		}
	}

	sm.offset = offset
	sm.path = file
	index.indexes = append(index.indexes, offset)
	return index, nil
}

// debugFlatSourceMap 返回展平后的标准 v3 Source Map。
func debugFlatSourceMap(script *Script, exposeSourceContent bool) (*SourceMap, error) {
	if script == nil || script.File == "" {
		return nil, nil
	}

	data, has := SourceMaps[script.File]
	if !has {
		return nil, nil
	}

	sections := []debugSourceMapSection{}
	offset := debugSourceMapOffset{}
	if runtimeOption.Import {
		if imports, has := ImportMap[script.File]; has {
			for i, imp := range imports {
				data, has := ModuleSourceMaps[imp.AbsPath]
				if !has {
					continue
				}

				module, has := Modules[imp.AbsPath]
				if !has {
					continue
				}

				sm, err := NewSourceMap(data)
				if err != nil {
					return nil, err
				}
				sections = append(sections, debugSourceMapSection{
					Offset: offset,
					Map:    debugSourceMap(sm, imp.Path, exposeSourceContent),
				})

				importCode := fmt.Sprintf("%s;const %s = %s;", module.Source, imp.Name, module.GlobalName)
				offset = advanceDebugSourceMapOffset(offset, importCode)
				if i < len(imports)-1 {
					offset = advanceDebugSourceMapOffset(offset, ";")
				}
			}
		}
	}

	sm, err := NewSourceMap(data)
	if err != nil {
		return nil, err
	}
	if script != nil && script.File != "" {
		sm.Sources = []string{script.File}
		log.Info("[V8 Debug] Setting main script source: %s, Mappings length: %d", script.File, len(sm.Mappings))
	}
	sections = append(sections, debugSourceMapSection{
		Offset: offset,
		Map:    debugSourceMap(sm, script.File, exposeSourceContent),
	})

	indexed := &debugIndexedSourceMap{
		Version:  3,
		File:     script.File,
		Sections: sections,
	}

	log.Info("[V8 Debug] indexed Sections count: %d", len(indexed.Sections))
	for idx, sec := range indexed.Sections {
		if sec.Map != nil {
			log.Info("[V8 Debug] Section %d Map File: %s, Sources: %v, Mappings length: %d", idx, sec.Map.File, sec.Map.Sources, len(sec.Map.Mappings))
		} else {
			log.Info("[V8 Debug] Section %d Map is nil", idx)
		}
	}

	flat, err := flattenDebugSourceMap(indexed)
	if err == nil && flat != nil {
		log.Info("[V8 Debug] Final Flat Sources: %v", flat.Sources)
	}
	return flat, err
}

func debugSourceMapBytes(script *Script, exposeSourceContent bool) ([]byte, error) {
	flat, err := debugFlatSourceMap(script, exposeSourceContent)
	if err != nil {
		return nil, err
	}
	if flat == nil {
		return nil, nil
	}
	return jsoniter.Marshal(flat)
}

func debugSourceMap(sm *SourceMap, fallbackFile string, exposeSourceContent bool) *SourceMap {
	if sm == nil {
		return nil
	}

	rawSources := sm.Sources
	if len(rawSources) == 0 {
		rawSources = []string{fallbackFile}
	}

	sourcePaths := make([]string, len(rawSources))
	sources := make([]string, len(rawSources))
	for i, source := range rawSources {
		sourcePaths[i] = debugSourcePath(source, sm.SourceRoot, fallbackFile)
		sources[i] = debugSourceURL(sourcePaths[i])
	}

	var sourcesContent []string
	if exposeSourceContent {
		sourcesContent = sm.SourcesContent
		if len(sourcesContent) == 0 && len(sources) > 0 {
			sourcesContent = make([]string, len(sources))
			for i, sourcePath := range sourcePaths {
				if content, err := os.ReadFile(sourcePath); err == nil {
					sourcesContent[i] = string(content)
				}
			}
		}
	}

	version := sm.Version
	if version == 0 {
		version = 3
	}

	return &SourceMap{
		Version:        version,
		File:           sm.File,
		Sources:        sources,
		Names:          sm.Names,
		Mappings:       sm.Mappings,
		SourcesContent: sourcesContent,
	}
}

func debugSourcePath(source string, sourceRoot string, fallbackFile string) string {
	if source == "" {
		source = fallbackFile
	}
	if sourceRoot != "" && !filepath.IsAbs(source) {
		source = filepath.Join(sourceRoot, source)
	}

	source = filepath.Clean(filepath.FromSlash(source))
	if filepath.IsAbs(source) {
		return source
	}

	if appRoot := debugAppRoot(); appRoot != "" {
		if appPath := debugAppRelativePath(source); appPath != "" {
			return filepath.Join(appRoot, appPath)
		}
		return filepath.Join(appRoot, source)
	}

	if filepath.IsAbs(fallbackFile) {
		return filepath.Join(filepath.Dir(fallbackFile), source)
	}
	return source
}

func debugSourceURL(source string) string {
	if source == "" {
		return source
	}
	if u, err := url.Parse(source); err == nil && u.Scheme != "" {
		return source
	}

	file := filepath.Clean(filepath.FromSlash(source))
	if filepath.IsAbs(file) {
		return (&url.URL{Scheme: "file", Path: file}).String()
	}
	return filepath.ToSlash(source)
}

func debugAppRoot() string {
	if application.App == nil {
		return ""
	}
	return application.App.Root()
}

func debugAppRelativePath(source string) string {
	source = filepath.ToSlash(source)
	parts := []string{
		"scripts/",
		"studio/",
		"apis/",
		"models/",
		"flows/",
		"services/",
		"tasks/",
		"schedules/",
	}
	for _, part := range parts {
		if idx := strings.Index(source, part); idx >= 0 {
			return filepath.FromSlash(source[idx:])
		}
	}
	return ""
}

func advanceDebugSourceMapOffset(offset debugSourceMapOffset, source string) debugSourceMapOffset {
	for _, ch := range source {
		if ch == '\n' {
			offset.Line++
			offset.Column = 0
			continue
		}
		offset.Column++
	}
	return offset
}

// NewSourceMap create a new source map
func NewSourceMap(data []byte) (*SourceMap, error) {
	var sourceMap SourceMap
	err := jsoniter.Unmarshal(data, &sourceMap)
	if err != nil {
		return nil, err
	}

	sourceMap.bytes = data
	sourceMap.offset = 0
	return &sourceMap, nil
}

func parseStackTrace(trace string) StackLogEntryList {
	res := []*StackLogEntry{}
	lines := strings.Split(trace, "\n")
	for _, line := range lines {
		match := reStackEntry.FindStringSubmatch(line)
		if match != nil {
			line, _ := strconv.Atoi(match[3])
			column, _ := strconv.Atoi(match[4])
			entry := &StackLogEntry{
				Function: match[1],
				File:     match[2],
				Line:     line,
				Column:   column,
			}
			res = append(res, entry)
		}
	}
	return res
}

func fmtFilePath(file string, rootMapping interface{}) string {
	file = strings.ReplaceAll(file, ".."+string(os.PathSeparator), "")
	if !strings.HasPrefix(file, string(os.PathSeparator)) {
		file = string(os.PathSeparator) + file
	}

	file = strings.TrimPrefix(file, application.App.Root())
	if rootMapping != nil {
		switch mapping := rootMapping.(type) {
		case map[string]string:
			for name, mappping := range mapping {
				if strings.HasPrefix(file, name) {
					file = mappping + strings.TrimPrefix(file, name)
					break
				}
			}
			break

		case func(string) string:
			file = mapping(file)
			break
		}
	}
	return file
}

func cntSource(source string) int {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	return strings.Count(source, "\n")
}

// vlqBase64Chars 是 VLQ Base64 编码字符表
const vlqBase64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// vlqDecodeSegment 解码一个 VLQ segment（逗号分隔的一个映射条目）
func vlqDecodeSegment(segment string) ([]int, error) {
	var values []int
	pos := 0
	for pos < len(segment) {
		value := 0
		shift := 0
		for {
			if pos >= len(segment) {
				return nil, fmt.Errorf("incomplete VLQ sequence")
			}
			idx := strings.IndexByte(vlqBase64Chars, segment[pos])
			if idx < 0 {
				return nil, fmt.Errorf("invalid VLQ character: %c", segment[pos])
			}
			pos++
			value += (idx & 0x1F) << shift
			shift += 5
			if (idx & 0x20) == 0 {
				break
			}
		}
		if (value & 1) != 0 {
			values = append(values, -(value >> 1))
		} else {
			values = append(values, value>>1)
		}
	}
	return values, nil
}

// vlqEncodeValue 将一个整数编码为 VLQ Base64 字符串
func vlqEncodeValue(value int) string {
	var buf [10]byte
	n := 0
	if value < 0 {
		value = (-value << 1) | 1
	} else {
		value <<= 1
	}
	for {
		digit := value & 0x1F
		value >>= 5
		if value > 0 {
			digit |= 0x20
		}
		buf[n] = vlqBase64Chars[digit]
		n++
		if value == 0 {
			break
		}
	}
	return string(buf[:n])
}

// flattenDebugSourceMap 将 Index Source Map 展平为标准 v3 Source Map。
// VSCode js-debug 对 Index Map 的 setBreakpointByUrl 行号反向映射存在兼容性问题，
// 使用标准 Source Map 可确保断点行号被正确解析。
func flattenDebugSourceMap(indexed *debugIndexedSourceMap) (*SourceMap, error) {
	if len(indexed.Sections) == 0 {
		return &SourceMap{Version: 3, File: indexed.File}, nil
	}

	// 单 section 且偏移为 0 时直接返回
	if len(indexed.Sections) == 1 {
		sec := indexed.Sections[0]
		if sec.Offset.Line == 0 && sec.Offset.Column == 0 && sec.Map != nil {
			return sec.Map, nil
		}
	}

	flat := &SourceMap{
		Version: 3,
		File:    indexed.File,
	}

	var mappingsBuf strings.Builder
	currentGenLine := 0
	prevGenCol := 0
	prevSourceIdx := 0
	prevSourceLine := 0
	prevSourceCol := 0
	prevNameIdx := 0

	for _, section := range indexed.Sections {
		sm := section.Map
		if sm == nil {
			continue
		}

		sourceIdxBase := len(flat.Sources)
		nameIdxBase := len(flat.Names)

		flat.Sources = append(flat.Sources, sm.Sources...)
		flat.SourcesContent = append(flat.SourcesContent, sm.SourcesContent...)
		flat.Names = append(flat.Names, sm.Names...)

		if sm.Mappings == "" {
			continue
		}

		// 推进到 section 的起始行
		targetLine := section.Offset.Line
		for currentGenLine < targetLine {
			mappingsBuf.WriteByte(';')
			currentGenLine++
			prevGenCol = 0
		}

		// section 内部的相对值累加器
		secSourceIdx := 0
		secSourceLine := 0
		secSourceCol := 0
		secNameIdx := 0

		lines := strings.Split(sm.Mappings, ";")
		for lineIdx, line := range lines {
			if lineIdx > 0 {
				mappingsBuf.WriteByte(';')
				currentGenLine++
				prevGenCol = 0
			}

			if line == "" {
				continue
			}

			segments := strings.Split(line, ",")
			lineGenCol := 0 // 每行内的生成列累加器（section 局部）

			firstSeg := true
			for _, seg := range segments {
				if seg == "" {
					continue
				}

				values, err := vlqDecodeSegment(seg)
				if err != nil {
					return nil, fmt.Errorf("vlq decode: %w", err)
				}
				if len(values) < 1 {
					continue
				}

				if !firstSeg {
					mappingsBuf.WriteByte(',')
				}
				firstSeg = false

				// 生成列：section 内逐段累加
				lineGenCol += values[0]
				absGenCol := lineGenCol
				// section 首行需要加上列偏移
				if lineIdx == 0 {
					absGenCol += section.Offset.Column
				}

				mappingsBuf.WriteString(vlqEncodeValue(absGenCol - prevGenCol))
				prevGenCol = absGenCol

				if len(values) >= 4 {
					secSourceIdx += values[1]
					secSourceLine += values[2]
					secSourceCol += values[3]

					absSourceIdx := secSourceIdx + sourceIdxBase
					mappingsBuf.WriteString(vlqEncodeValue(absSourceIdx - prevSourceIdx))
					mappingsBuf.WriteString(vlqEncodeValue(secSourceLine - prevSourceLine))
					mappingsBuf.WriteString(vlqEncodeValue(secSourceCol - prevSourceCol))

					prevSourceIdx = absSourceIdx
					prevSourceLine = secSourceLine
					prevSourceCol = secSourceCol

					if len(values) >= 5 {
						secNameIdx += values[4]
						absNameIdx := secNameIdx + nameIdxBase
						mappingsBuf.WriteString(vlqEncodeValue(absNameIdx - prevNameIdx))
						prevNameIdx = absNameIdx
					}
				}
			}
		}
	}

	flat.Mappings = mappingsBuf.String()
	return flat, nil
}

// matchSourceByURL 通过直接 URL 匹配 source map 中的源文件。
func matchSourceByURL(sources []string, cdpURL string) int {
	for i, source := range sources {
		if debugSourceMatchesURL(source, cdpURL) {
			return i
		}
	}
	return -1
}

func debugSourceMatchesURL(source string, cdpURL string) bool {
	if source == cdpURL {
		return true
	}

	sourceURL := debugSourceURL(source)
	if sourceURL == cdpURL {
		return true
	}

	sourcePath := debugSourceFilePath(source)
	cdpPath := debugSourceFilePath(cdpURL)
	return sourcePath != "" && cdpPath != "" && sourcePath == cdpPath
}

func debugSourceFilePath(source string) string {
	if source == "" {
		return ""
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		return filepath.Clean(filepath.FromSlash(u.Path))
	}
	if u, err := url.Parse(source); err == nil && u.Scheme != "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(source))
}

// matchSourceByRegex 通过 CDP urlRegex 匹配 source map 中的源文件。
func matchSourceByRegex(sources []string, urlRegex string) int {
	re, err := regexp.Compile(urlRegex)
	if err != nil {
		return -1
	}
	for i, source := range sources {
		sourceURL := debugSourceURL(source)
		if re.MatchString(sourceURL) {
			return i
		}
	}
	return -1
}

// reverseMapSourcePosition 在扁平 source map 中查找给定源文件行号对应的编译行号。
// targetSourceIdx 是 sources 数组索引，targetSourceLine 是 0-indexed 源行号。
// 返回编译行号（0-indexed）和是否找到。
func reverseMapSourcePosition(sm *SourceMap, targetSourceIdx int, targetSourceLine int) (int, bool) {
	if sm == nil || sm.Mappings == "" {
		return 0, false
	}

	lines := strings.Split(sm.Mappings, ";")
	curSrcIdx, curSrcLine := 0, 0

	for genLine, line := range lines {
		if line == "" {
			continue
		}
		for _, seg := range strings.Split(line, ",") {
			if seg == "" {
				continue
			}
			values, err := vlqDecodeSegment(seg)
			if err != nil {
				continue
			}
			if len(values) >= 4 {
				curSrcIdx += values[1]
				curSrcLine += values[2]
				if curSrcIdx == targetSourceIdx && curSrcLine == targetSourceLine {
					return genLine, true
				}
			}
		}
	}
	return 0, false
}
