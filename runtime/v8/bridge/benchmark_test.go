package bridge

import (
	"testing"

	jsoniter "github.com/json-iterator/go"
	"rogchap.com/v8go"
)

func BenchmarkBridgeJsValue_ComplexObject(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()
	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// 模拟从数据库查出来的包含 20 条复杂订单记录的结果集
	records := make([]map[string]interface{}, 20)
	for i := 0; i < 20; i++ {
		records[i] = map[string]interface{}{
			"id":          1000 + i,
			"order_no":    "ORD20260918000" + string(rune('0'+i)),
			"user_id":     20001,
			"amount":      199.50,
			"status":      "paid",
			"created_at":  "2026-09-18 23:59:00",
			"items_count": 3,
		}
	}
	data := map[string]interface{}{
		"total": 20,
		"page":  1,
		"data":  records,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		val, err := JsValue(ctx, data)
		if err != nil {
			b.Fatal(err)
		}
		val.Release()
	}
}

func BenchmarkBridge_100Records_Comparison(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()
	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// 模拟 100 条完整订单结构
	records := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		records[i] = map[string]interface{}{
			"id":          1000 + i,
			"order_no":    "ORD20260918000" + string(rune('0'+(i%10))),
			"user_id":     20001,
			"amount":      199.50,
			"status":      "paid",
			"created_at":  "2026-09-18 23:59:00",
			"items_count": 3,
			"remark":      "standard patient consultation fee payment record",
		}
	}
	data := map[string]interface{}{
		"total": 100,
		"page":  1,
		"data":  records,
	}

	b.Run("Old_Path_StringParse_100Records", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			raw, _ := jsoniter.Marshal(data)
			val, err := v8go.JSONParse(ctx, string(raw)) // 旧路径：string + C.CString
			if err != nil {
				b.Fatal(err)
			}
			val.Release()
		}
	})

	b.Run("Optimized_Path_BytesParse_100Records", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			raw, _ := jsoniter.Marshal(data)
			val, err := v8go.JSONParseBytes(ctx, raw) // 新路径：直接切片指针进 CGO
			if err != nil {
				b.Fatal(err)
			}
			val.Release()
		}
	})
}

func BenchmarkBridge_GoValue_100Records(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()
	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	records := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		records[i] = map[string]interface{}{
			"id":          1000 + i,
			"order_no":    "ORD20260918000" + string(rune('0'+(i%10))),
			"user_id":     20001,
			"amount":      199.50,
			"status":      "paid",
			"created_at":  "2026-09-18 23:59:00",
			"items_count": 3,
			"remark":      "standard patient consultation fee payment record",
		}
	}
	data := map[string]interface{}{
		"total": 100,
		"page":  1,
		"data":  records,
	}

	raw, _ := jsoniter.Marshal(data)
	jsVal, err := v8go.JSONParseBytes(ctx, raw)
	if err != nil {
		b.Fatal(err)
	}
	defer jsVal.Release()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		goVal, err := GoValue(jsVal, ctx)
		if err != nil {
			b.Fatal(err)
		}
		if goVal == nil {
			b.Fatal("unexpected nil")
		}
	}
}

func BenchmarkBridge_RoundTrip_100Records(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()
	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	records := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		records[i] = map[string]interface{}{
			"id":          1000 + i,
			"order_no":    "ORD20260918000" + string(rune('0'+(i%10))),
			"user_id":     20001,
			"amount":      199.50,
			"status":      "paid",
			"created_at":  "2026-09-18 23:59:00",
			"items_count": 3,
			"remark":      "standard patient consultation fee payment record",
		}
	}
	data := map[string]interface{}{
		"total": 100,
		"page":  1,
		"data":  records,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Go -> JS
		jsVal, err := JsValue(ctx, data)
		if err != nil {
			b.Fatal(err)
		}
		// JS -> Go
		goVal, err := GoValue(jsVal, ctx)
		if err != nil {
			b.Fatal(err)
		}
		if goVal == nil {
			b.Fatal("unexpected nil")
		}
		jsVal.Release()
	}
}
