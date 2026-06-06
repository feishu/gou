package v8

import (
	"strings"
	"testing"
)

func TestVlqEncodeDecodeRoundtrip(t *testing.T) {
	cases := []int{0, 1, -1, 5, -5, 15, -15, 16, 100, -100, 1000, -1000, 10000}
	for _, want := range cases {
		encoded := vlqEncodeValue(want)
		decoded, err := vlqDecodeSegment(encoded)
		if err != nil {
			t.Fatalf("decode(%q) error: %v", encoded, err)
		}
		if len(decoded) != 1 || decoded[0] != want {
			t.Fatalf("roundtrip(%d): encoded=%q, decoded=%v", want, encoded, decoded)
		}
	}
}

func TestVlqDecodeKnownValues(t *testing.T) {
	// "AAAA" = [0, 0, 0, 0]
	values, err := vlqDecodeSegment("AAAA")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 4 {
		t.Fatalf("expected 4 values, got %d", len(values))
	}
	for i, v := range values {
		if v != 0 {
			t.Fatalf("values[%d] = %d, want 0", i, v)
		}
	}

	// "CAAG" = [1, 0, 0, 3]
	values, err = vlqDecodeSegment("CAAG")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 4 || values[0] != 1 || values[1] != 0 || values[2] != 0 || values[3] != 3 {
		t.Fatalf("CAAG = %v, expected [1,0,0,3]", values)
	}
}

func TestVlqEncodeZero(t *testing.T) {
	if got := vlqEncodeValue(0); got != "A" {
		t.Fatalf("vlqEncodeValue(0) = %q, want %q", got, "A")
	}
}

func TestFlattenEmptySections(t *testing.T) {
	indexed := &debugIndexedSourceMap{
		Version:  3,
		File:     "test.ts",
		Sections: nil,
	}
	flat, err := flattenDebugSourceMap(indexed)
	if err != nil {
		t.Fatal(err)
	}
	if flat.Version != 3 {
		t.Fatalf("expected version 3, got %d", flat.Version)
	}
	if flat.Mappings != "" {
		t.Fatalf("expected empty mappings, got %q", flat.Mappings)
	}
}

func TestFlattenSingleSectionAtOrigin(t *testing.T) {
	sm := &SourceMap{
		Version:        3,
		Sources:        []string{"/src/a.ts"},
		SourcesContent: []string{"const a = 1;"},
		Mappings:       "AAAA",
	}
	indexed := &debugIndexedSourceMap{
		Version: 3,
		File:    "a.ts",
		Sections: []debugSourceMapSection{
			{Offset: debugSourceMapOffset{Line: 0, Column: 0}, Map: sm},
		},
	}
	flat, err := flattenDebugSourceMap(indexed)
	if err != nil {
		t.Fatal(err)
	}
	// 单 section 在原点时应直接返回
	if flat.Mappings != "AAAA" {
		t.Fatalf("expected direct passthrough, got mappings=%q", flat.Mappings)
	}
	if len(flat.Sources) != 1 || flat.Sources[0] != "/src/a.ts" {
		t.Fatalf("unexpected sources: %v", flat.Sources)
	}
}

func TestFlattenTwoSectionsWithOffset(t *testing.T) {
	// section 0: offset (0,0), source "a.ts", mappings "AAAA"
	// section 1: offset (5,10), source "b.ts", mappings "AAAA"
	//
	// 期望：展平后 mappings 中 section 1 的映射从第 5 行开始，
	// 且第一个 segment 的生成列偏移了 10。
	sectionA := &SourceMap{
		Version:        3,
		Sources:        []string{"/a.ts"},
		SourcesContent: []string{"a"},
		Mappings:       "AAAA",
	}
	sectionB := &SourceMap{
		Version:        3,
		Sources:        []string{"/b.ts"},
		SourcesContent: []string{"b"},
		Mappings:       "AAAA",
	}
	indexed := &debugIndexedSourceMap{
		Version: 3,
		File:    "bundle.js",
		Sections: []debugSourceMapSection{
			{Offset: debugSourceMapOffset{Line: 0, Column: 0}, Map: sectionA},
			{Offset: debugSourceMapOffset{Line: 5, Column: 10}, Map: sectionB},
		},
	}

	flat, err := flattenDebugSourceMap(indexed)
	if err != nil {
		t.Fatal(err)
	}

	// 验证 sources 已合并
	if len(flat.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(flat.Sources))
	}
	if flat.Sources[0] != "/a.ts" || flat.Sources[1] != "/b.ts" {
		t.Fatalf("unexpected sources: %v", flat.Sources)
	}

	// 验证 mappings 中有足够的 ';' 行分隔符
	lines := strings.Split(flat.Mappings, ";")
	if len(lines) < 6 {
		t.Fatalf("expected at least 6 lines in mappings, got %d (mappings=%q)", len(lines), flat.Mappings)
	}

	// 第 0 行 (section A) 应有内容
	if lines[0] == "" {
		t.Fatal("expected section A mappings on line 0")
	}

	// 第 1-4 行应该为空（section A 只有 1 行，section B 从第 5 行开始）
	for i := 1; i < 5; i++ {
		if lines[i] != "" {
			t.Fatalf("expected empty line %d, got %q", i, lines[i])
		}
	}

	// 第 5 行 (section B) 应有内容
	if lines[5] == "" {
		t.Fatal("expected section B mappings on line 5")
	}

	// 解码第 5 行的第一个 segment，验证生成列包含偏移 10
	seg := strings.Split(lines[5], ",")[0]
	values, err := vlqDecodeSegment(seg)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) < 1 {
		t.Fatal("expected at least 1 value in segment")
	}
	// 生成列 = section 内局部列(0) + section 列偏移(10) = 10
	if values[0] != 10 {
		t.Fatalf("expected generated column 10 (with offset), got %d", values[0])
	}

	// 如果有 source index，验证它指向 source index 1（第二个源文件 b.ts）
	if len(values) >= 4 {
		// source index 是相对值，相对于之前的累计值
		// section A 的最后 source index 是 0，所以 section B 的 source index 相对值应该是 1
		if values[1] != 1 {
			t.Fatalf("expected relative source index 1 (b.ts), got %d", values[1])
		}
	}
}

func TestFlattenPreservesSourcesContent(t *testing.T) {
	sectionA := &SourceMap{
		Version:        3,
		Sources:        []string{"/a.ts"},
		SourcesContent: []string{"content-a"},
		Mappings:       "AAAA",
	}
	sectionB := &SourceMap{
		Version:        3,
		Sources:        []string{"/b.ts"},
		SourcesContent: []string{"content-b"},
		Mappings:       "AAAA",
	}
	indexed := &debugIndexedSourceMap{
		Version: 3,
		File:    "bundle.js",
		Sections: []debugSourceMapSection{
			{Offset: debugSourceMapOffset{Line: 0, Column: 0}, Map: sectionA},
			{Offset: debugSourceMapOffset{Line: 10, Column: 0}, Map: sectionB},
		},
	}

	flat, err := flattenDebugSourceMap(indexed)
	if err != nil {
		t.Fatal(err)
	}

	if len(flat.SourcesContent) != 2 {
		t.Fatalf("expected 2 sourcesContent, got %d", len(flat.SourcesContent))
	}
	if flat.SourcesContent[0] != "content-a" {
		t.Fatalf("unexpected sourcesContent[0]: %s", flat.SourcesContent[0])
	}
	if flat.SourcesContent[1] != "content-b" {
		t.Fatalf("unexpected sourcesContent[1]: %s", flat.SourcesContent[1])
	}
}
