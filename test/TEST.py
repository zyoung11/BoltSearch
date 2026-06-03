import json
import requests
from PAT import get, post, delete, run_test, show_result, print_info

BASE = "http://127.0.0.1:8080"

DEMO = [
    {"title": "小米14 Pro深度评测", "content": "小米14 Pro搭载高通骁龙8 Gen 3处理器，采用台积电4nm工艺。屏幕为6.73英寸1.5K分辨率AMOLED面板，支持120Hz LTPO自适应刷新率，峰值亮度3000nit。后置三摄：5000万像素主摄、5000万像素超广角、5000万像素长焦。电池4610mAh，支持90W有线快充和50W无线快充。"},
    {"title": "华为Mate 60 Pro体验", "content": "华为Mate 60 Pro搭载自研麒麟9000S处理器，支持卫星通话和卫星消息。采用6.82英寸OLED四曲面屏，支持1-120Hz自适应刷新率。后置5000万像素主摄+1200万像素超广角+4800万像素长焦微距。电池4750mAh，支持88W有线快充和50W无线快充。"},
    {"title": "iPhone 15 Pro Max开箱", "content": "iPhone 15 Pro Max搭载A17 Pro芯片，基于台积电3nm工艺制造。采用6.7英寸Super Retina XDR OLED显示屏，支持ProMotion 120Hz自适应刷新率。后置4800万像素主摄+1200万像素超广角+1200万像素5倍长焦。首次采用USB-C接口，钛金属中框。"},
    {"title": "Python编程入门指南", "content": "Python is a high-level, interpreted programming language known for its readability and simple syntax. It supports multiple programming paradigms including object-oriented, imperative, and functional programming."},
    {"title": "Golang并发编程实战", "content": "Golang is a statically typed, compiled programming language designed at Google. Key features include goroutines for lightweight concurrency, channels for communication between goroutines, and a built-in garbage collector."},
    {"title": "Rust语言入门与内存安全", "content": "Rust is a systems programming language focused on safety, speed, and concurrency. Its unique ownership system prevents memory errors at compile time without needing a garbage collector."},
    {"title": "骁龙8 Gen 3 vs 天玑9300 性能对比", "content": "高通骁龙8 Gen 3采用1+5+2三丛集架构，Cortex-X4超大核主频3.3GHz，GPU为Adreno 750，台积电4nm工艺。安兔兔跑分约210万分。联发科天玑9300采用4个Cortex-X4超大核+4个Cortex-A720大核的激进设计，台积电4nm工艺。"},
    {"title": "2024年最佳智能手机推荐", "content": "年度智能手机推荐榜单：小米14 Pro以出色的徕卡影像和骁龙8 Gen 3性能位居榜首。华为Mate 60 Pro凭借卫星通信和自研麒麟芯片获得特别推荐。iPhone 15 Pro Max以A17 Pro的强大性能和优秀的视频拍摄能力占据高端市场。"},
    {"title": "Redis缓存设计与性能优化", "content": "Redis is an in-memory data structure store used as a database, cache, and message broker. Common data structures include strings, hashes, lists, sets, sorted sets, and streams."},
    {"title": "PostgreSQL索引优化技巧", "content": "PostgreSQL supports multiple index types: B-tree for equality and range queries, Hash for simple equality, GiST for geometric and full-text search, GIN for array and JSONB queries, and BRIN for large tables with natural ordering."},
]


def test_index():
    jsonl = "\n".join(json.dumps(d, ensure_ascii=False) for d in DEMO)
    files = {"file": ("demo.jsonl", jsonl.encode(), "application/jsonl")}
    resp = requests.post(f"{BASE}/api/index", files=files, timeout=10)
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
    print_info("服务信息", {"URL": BASE})

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
