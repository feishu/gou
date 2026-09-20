package helper

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/yaoapp/kun/any"
	"github.com/yaoapp/kun/maps"
)

var reVar = regexp.MustCompile("{{[ ]*([^\\s]+)[ ]*}}")                     // {{in.2}}
var reVarStyle2 = regexp.MustCompile("\\?:([^\\s^%]+)")                     // ?:$in.2
var reFun = regexp.MustCompile("{{[ ]*([0-9a-zA-Z_]+)[ ]*\\((.*)\\)[ ]*}}") // {{pluck($res.users, 'id')}}
var reFunArg = regexp.MustCompile("([^\\s,]+)")                             // $res.users, 'id'

// Bind 绑定数据
func Bind(v interface{}, data map[string]interface{}, vars ...*regexp.Regexp) interface{} {
	if len(vars) == 0 {
		vars = []*regexp.Regexp{reVar, reVarStyle2}
	}

	var res interface{}

	// If the value is a []byte type, return it directly
	if bytes, ok := v.([]byte); ok {
		return bytes
	}

	value := reflect.ValueOf(v)
	value = reflect.Indirect(value)
	valueKind := value.Kind()
	if valueKind == reflect.Interface {
		value = value.Elem()
		valueKind = value.Kind()
	}

	switch valueKind {
	case reflect.Slice, reflect.Array: // Slice || Array
		val := make([]interface{}, value.Len())
		for i := 0; i < value.Len(); i++ {
			val[i] = Bind(value.Index(i).Interface(), data)
		}
		res = val
	case reflect.Map: // Map
		val := make(map[string]interface{})
		for _, key := range value.MapKeys() {
			k := fmt.Sprintf("%s", key)
			val[k] = Bind(value.MapIndex(key).Interface(), data)
		}
		res = val
	case reflect.String: // String
		input := value.Interface().(string)

		// 替换变量
		for _, re := range vars {
			matches := re.FindAllStringSubmatchIndex(input, -1)
			length := len(matches)
			if length == 1 { // "{{in.0}}"
				name := input[matches[0][2]:matches[0][3]]
				val, exists := getValue(data, name)
				if exists {
					res = val
					// Replace the string if the value is a string
					if v, ok := val.(string); ok {
						orignal := input[matches[0][0]:matches[0][1]]
						res = strings.Replace(input, orignal, fmt.Sprintf("%v", v), 1)
					}
				} else {
					res = nil
				}
				break
			} else if length > 1 {
				var sb strings.Builder
				lastIndex := 0
				for _, match := range matches {
					name := input[match[2]:match[3]]
					val, exists := getValue(data, name)
					var valStr string
					if exists {
						valStr = fmt.Sprintf("%v", val)
					}
					sb.WriteString(input[lastIndex:match[0]])
					sb.WriteString(valStr)
					lastIndex = match[1]
				}
				sb.WriteString(input[lastIndex:])
				res = sb.String()
				break
			} else {
				res = input
			}
		}
	default:
		res = v
	}

	return res
}

func extraFunArgs(input string, data maps.Map) []interface{} {
	args := []interface{}{}
	matches := reFunArg.FindAllStringSubmatch(input, -1)
	for _, match := range matches {
		key := match[1]
		keyAny := any.Of(key)
		if strings.HasPrefix(key, ":") {
			key = key[1:]
			val, _ := getValue(data, key)
			args = append(args, val)
		} else if strings.HasPrefix(key, "'") && strings.HasSuffix(key, "'") {
			args = append(args, strings.Trim(key, "'"))
		} else if strings.Contains(key, ".") {
			args = append(args, keyAny.CFloat())
		} else {
			args = append(args, keyAny.CInt())
		}
	}
	return args
}

// getValue 按需获取变量，优先直接查找，未命中则按点分/数组下标路径单点寻址
func getValue(data map[string]interface{}, path string) (interface{}, bool) {
	if data == nil {
		return nil, false
	}
	if v, exists := data[path]; exists {
		return v, true
	}
	if !strings.ContainsAny(path, ".[") {
		return nil, false
	}
	return getByPath(data, path)
}

// parsePathTokens 极速分词解析路径（支持 a.b.c 与 a[0].b 语法，零多余内存开销）
func parsePathTokens(path string) []string {
	tokens := make([]string, 0, 4)
	start := 0
	n := len(path)
	for i := 0; i < n; i++ {
		c := path[i]
		if c == '.' || c == '[' || c == ']' {
			if i > start {
				tokens = append(tokens, path[start:i])
			}
			start = i + 1
		}
	}
	if start < n {
		tokens = append(tokens, path[start:n])
	}
	return tokens
}

// getByPath 沿层级路径按需单点寻址，彻底消灭全量递归 Dot() 反射遍历
func getByPath(data map[string]interface{}, path string) (interface{}, bool) {
	tokens := parsePathTokens(path)
	if len(tokens) == 0 {
		return nil, false
	}

	var current interface{} = data
	for _, token := range tokens {
		if current == nil {
			return nil, false
		}
		switch val := current.(type) {
		case map[string]interface{}:
			v, ok := val[token]
			if !ok {
				return nil, false
			}
			current = v
		case []interface{}:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(val) {
				return nil, false
			}
			current = val[idx]
		case []map[string]interface{}:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(val) {
				return nil, false
			}
			current = val[idx]
		case []maps.MapStr:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(val) {
				return nil, false
			}
			current = val[idx]
		default:
			// 反射兜底
			rv := reflect.ValueOf(current)
			rv = reflect.Indirect(rv)
			switch rv.Kind() {
			case reflect.Map:
				mv := rv.MapIndex(reflect.ValueOf(token))
				if !mv.IsValid() {
					return nil, false
				}
				current = mv.Interface()
			case reflect.Slice, reflect.Array:
				idx, err := strconv.Atoi(token)
				if err != nil || idx < 0 || idx >= rv.Len() {
					return nil, false
				}
				current = rv.Index(idx).Interface()
			case reflect.Struct:
				fv := rv.FieldByName(token)
				if !fv.IsValid() {
					return nil, false
				}
				current = fv.Interface()
			default:
				return nil, false
			}
		}
	}
	return current, true
}
