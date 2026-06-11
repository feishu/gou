package v8

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yaoapp/gou/application"
)

// GetFileName get the file name from the tsconfig
func (tsconfg *TSConfig) GetFileName(path string) (string, bool, error) {
	if tsconfg == nil {
		return path, false, nil
	}

	if file, match, has := tsconfg.cached(path); has {
		return file, match, nil
	}

	if tsconfg.CompilerOptions == nil || tsconfg.CompilerOptions.Paths == nil {
		tsconfg.cache(path, path, false)
		return path, false, nil
	}

	for pattern, paths := range tsconfg.CompilerOptions.Paths {
		if tsconfg.Match(pattern, path) {
			for _, p := range paths {
				matched := false
				dir := filepath.Clean(filepath.Dir(p))
				f := filepath.Join(dir, tsconfg.ReplacePattern(path, pattern))
				err := application.App.Walk(dir, func(root, filename string, isdir bool) error {
					if isdir {
						return nil
					}
					if filename == f {
						matched = true
						return filepath.SkipAll
					}
					return nil
				}, "*.ts")

				if matched {
					tsconfg.cache(path, f, true)
					return f, true, nil
				}

				if err == nil {
					tsconfg.cache(path, path, false)
					return path, false, nil
				}
			}
		}
	}
	tsconfg.cache(path, path, false)
	return path, false, nil
}

func (tsconfg *TSConfig) cached(path string) (string, bool, bool) {
	tsconfg.cacheMu.RLock()
	defer tsconfg.cacheMu.RUnlock()
	if tsconfg.pathCache == nil {
		return "", false, false
	}
	item, has := tsconfg.pathCache[path]
	if !has {
		return "", false, false
	}
	return item.file, item.match, true
}

func (tsconfg *TSConfig) cache(path string, file string, match bool) {
	tsconfg.cacheMu.Lock()
	defer tsconfg.cacheMu.Unlock()
	if tsconfg.pathCache == nil {
		tsconfg.pathCache = map[string]tsConfigPathCache{}
	}
	tsconfg.pathCache[path] = tsConfigPathCache{file: file, match: match}
}

func (tsconfg *TSConfig) clearCache() {
	tsconfg.cacheMu.Lock()
	defer tsconfg.cacheMu.Unlock()
	tsconfg.pathCache = nil
}

// Match match the pattern
func (tsconfg *TSConfig) Match(pattern, path string) bool {
	prefix := strings.Split(pattern, "/*")[0] + string(os.PathSeparator)
	return strings.HasPrefix(path, prefix)
}

// ReplacePattern replace the pattern
func (tsconfg *TSConfig) ReplacePattern(path, pattern string) string {
	prefix := strings.Split(pattern, "/*")[0]
	file := strings.TrimPrefix(path, prefix)
	if strings.HasSuffix(file, ".ts") {
		return file
	}
	return file + ".ts"
}
