import requests
from pathlib import Path
from PAT import get, post, delete, run_test, show_result, print_info

BASE = "http://127.0.0.1:8080"
EXAMPLE = Path(__file__).parent.parent / "example.jsonl"


def test_index():
    with EXAMPLE.open("rb") as f:
        resp = requests.post(f"{BASE}/api/index", files={"file": f}, timeout=10)
    ok = 200 <= resp.status_code < 300
    status = "✅" if ok else "❌"
    content = resp.json() if ok else {"error": resp.text}
    return run_test("上传索引文件", (status, content, resp.status_code, None))


def test_stats():
    return run_test("获取统计信息", get(f"{BASE}/api/stats"))


def test_search():
    return run_test("搜索'处理器'", get(f"{BASE}/api/search?q=处理器&limit=2"))


def test_search_and():
    return run_test("AND搜索'小米 骁龙'", get(f"{BASE}/api/search?q=小米 骁龙&mode=and"))


def test_search_prefix():
    return run_test("前缀搜索'处理'", get(f"{BASE}/api/search?q=处理&prefix=true"))


def test_search_english():
    return run_test("英文搜索'programming'", get(f"{BASE}/api/search?q=programming"))


def test_suggest():
    return run_test("补全'骁龙'", get(f"{BASE}/api/suggest?prefix=骁龙"))


def test_suggest_english():
    return run_test("补全'pro'", get(f"{BASE}/api/suggest?prefix=pro"))


def test_browse():
    return run_test("浏览 Bucket 列表", get(f"{BASE}/api/browse"))


def test_browse_docs():
    return run_test("浏览 docs Bucket", get(f"{BASE}/api/browse?bucket=docs&limit=3"))


def test_browse_index():
    return run_test("浏览 index Bucket", get(f"{BASE}/api/browse?bucket=index&limit=5"))


def test_get_doc():
    return run_test("获取 DocID=1", get(f"{BASE}/api/docs/1"))


def test_get_nonexistent():
    return run_test("获取不存在的 DocID=999", get(f"{BASE}/api/docs/999", should_fail=True))


def test_add_doc():
    return run_test(
        "手动添加新文档",
        post(f"{BASE}/api/docs", {"title": "测试文档", "content": "这是一篇通过 API 添加的测试文档"}),
    )


def test_add_duplicate():
    return run_test(
        "添加重复文档应返回409",
        post(
            f"{BASE}/api/docs",
            {"title": "测试文档", "content": "这是一篇通过 API 添加的测试文档"},
            should_fail=True,
        ),
    )


def test_delete():
    return run_test("删除 DocID=5", delete(f"{BASE}/api/docs/5"))


def test_delete_nonexistent():
    return run_test("删除不存在的 DocID=999", delete(f"{BASE}/api/docs/999", should_fail=True))


def test_empty_search():
    return run_test("空查询应返回400", get(f"{BASE}/api/search", should_fail=True))


if __name__ == "__main__":
    print_info("服务信息", {"URL": BASE, "数据文件": str(EXAMPLE)})

    test_index()
    test_stats()
    test_search()
    test_search_and()
    test_search_prefix()
    test_search_english()
    test_suggest()
    test_suggest_english()
    test_browse()
    test_browse_docs()
    test_browse_index()
    test_get_doc()
    test_get_nonexistent()
    test_add_doc()
    test_add_duplicate()
    test_delete()
    test_delete_nonexistent()
    test_empty_search()

    show_result("BoltSearch API 测试")
