package engine

import (
	"path/filepath"
	"testing"
)

func openTestEngine(t *testing.T, docs []Document) *SearchEngine {
	t.Helper()
	eng, err := NewSearchEngine(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	for _, doc := range docs {
		if _, err := eng.AddDocument(doc); err != nil {
			t.Fatal(err)
		}
	}
	return eng
}

func testDocs() []Document {
	return []Document{
		{
			Title:   "会计科目 › 10210501",
			Content: "科目: 会计科目\n科目代码: 10210501\n科目名称: 第三方支付平台账户存款\n核算内容: 本科目核算学校通过支付宝、微信等方式取得相关收入，尚未转入银行存款的支付宝、微信收付款等第三方支付平台账户的款项。学校通过收费系统进行管理的即时结算的收款业务。可凭银行进账单在“银行存款”科目核算，不通过本科目核算。",
			Meta:    map[string]string{"row": "row-17"},
		},
		{
			Title:   "会计科目 › 10010302",
			Content: "科目: 会计科目\n科目代码: 10010302\n科目名称: 其他货币资金\n核算内容: 本科目核算企业收到的其他货币资金。",
			Meta:    map[string]string{"row": "row-6"},
		},
		{
			Title:   "部门经济分类 › 3级",
			Content: "分类: 部门经济分类\n说明: 培训学习相关的费用支出分类。",
			Meta:    map[string]string{"row": "row-132"},
		},
	}
}

func testSearchOptions() SearchOptions {
	return SearchOptions{Mode: "or", Limit: 5, Prefix: true, Fuzzy: true, Phrase: true}
}

func firstRow(t *testing.T, eng *SearchEngine, query string) string {
	t.Helper()
	results, _, err := eng.Search(query, testSearchOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatalf("query %q 无结果", query)
	}
	return results[0].Doc.Meta["row"]
}

func TestQueryExactPhrase(t *testing.T) {
	eng := openTestEngine(t, testDocs())
	if row := firstRow(t, eng, "本科目核算学校通过支付宝"); row != "row-17" {
		t.Fatalf("精确短语命中 = %q, want row-17", row)
	}
}

func TestQueryPhraseWithTypo(t *testing.T) {
	eng := openTestEngine(t, testDocs())
	if row := firstRow(t, eng, "科目核算学习"); row != "row-17" {
		t.Fatalf("错字短语命中 = %q, want row-17", row)
	}
}

func TestQueryPhraseScrambled(t *testing.T) {
	eng := openTestEngine(t, testDocs())
	if row := firstRow(t, eng, "学校支付宝收入"); row != "row-17" {
		t.Fatalf("乱序短语命中 = %q, want row-17", row)
	}
}

func TestQueryPhraseNotDominant(t *testing.T) {
	eng := openTestEngine(t, testDocs())
	results, _, err := eng.Search("培训学习费用", testSearchOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("无结果")
	}
	if row := results[0].Doc.Meta["row"]; row != "row-132" {
		t.Fatalf("词项主导的查询命中 = %q, want row-132", row)
	}
}
