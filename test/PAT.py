import requests
import json
from typing import Optional, Any, Tuple, Dict, Union
from rich.console import Console
from rich.panel import Panel
from rich.syntax import Syntax
from rich.table import Table
from rich import box

# 全局变量存储测试结果
_test_results: Dict[str, Tuple[str, bool]] = {}


def _get_status_color(status_code: int) -> str:
    if 200 <= status_code < 300:
        return "green"
    if 400 <= status_code < 500:
        return "yellow"
    if 500 <= status_code < 600:
        return "red"
    return "white"


def print_info(title: str, info: Dict[str, Any]):
    console = Console()

    table = Table(
        show_header=True, header_style="magenta", box=box.ROUNDED, expand=True
    )
    table.add_column("Key", style="dim", width=20)
    table.add_column("Value")

    for k, v in info.items():
        table.add_row(str(k), str(v))

    console.print(Panel(table, title=title, border_style="green", expand=True))


def show_result(title: str = "测试结果汇总", save_to_md: bool = False):
    if not _test_results:
        console = Console()
        console.print("[yellow]没有测试结果可显示[/yellow]")
        return

    console = Console()

    table = Table(
        show_header=True, header_style="magenta", box=box.ROUNDED, expand=True
    )
    table.add_column("测试描述", style="dim", width=30)
    table.add_column("结果", width=10)

    success_count = 0
    fail_count = 0
    results_data = []

    for description, (status, is_success, _) in _test_results.items():
        if is_success:
            table.add_row(description, f"[green]{status} 成功[/green]")
            success_count += 1
        else:
            table.add_row(description, f"[red]{status} 失败[/red]")
            fail_count += 1

        # 收集结果数据用于保存到文件
        results_data.append({
            "测试描述": description,
            "结果": f"{status} {'成功' if is_success else '失败'}",
        })

    table.add_row("", "")
    table.add_row(
        "[bold]总计[/bold]",
        f"[green]成功: {success_count}[/green] | [red]失败: {fail_count}[/red]",
    )

    console.print(Panel(table, title=title, border_style="cyan", expand=True))

    # 如果需要保存到文件
    if save_to_md:
        console.print("[green]✓ 结果已保存到 ./Result.md[/green]\n")
        with open("./Result.md", "w", encoding="utf-8") as f:
            f.write(f"# {title}\n\n")

            # 测试描述部分
            f.write("## 测试描述\n\n")
            f.write("| 测试描述 | 结果 |\n")
            f.write("|----------|------|\n")
            for item in results_data:
                f.write(f"| {item['测试描述']} | {item['结果']} |\n")
            f.write("\n")

            # 结果部分
            f.write("## 结果\n\n")
            f.write(f"- **成功**: {success_count}\n")
            f.write(f"- **失败**: {fail_count}\n")
            f.write("\n")

            # API返回的json信息部分
            f.write("## API返回的json信息\n\n")
            for description, (status, is_success, content) in _test_results.items():
                f.write(f"### {description}\n\n")
                f.write(f"**状态**: {status} {'成功' if is_success else '失败'}\n\n")

                # 保存API返回的json信息
                if isinstance(content, (dict, list)):
                    json_str = json.dumps(content, indent=2, ensure_ascii=False)
                    f.write("```json\n")
                    f.write(json_str)
                    f.write("\n```\n\n")
                else:
                    f.write(f"{content}\n\n")

    _test_results.clear()


def clear_test_results():
    _test_results.clear()


def _deep_get(obj: Any, path: str) -> Any:
    keys = path.split(".")
    cur = obj
    for k in keys:
        if isinstance(cur, dict):
            cur = cur.get(k)
        elif isinstance(cur, list) and k.isdigit():
            cur = cur[int(k)]
        else:
            return None
        if cur is None:
            break
    return cur


def run_test(
    description: str, response: Tuple[str, Any, int, Optional[str]], *extract_paths: str
) -> Any:
    console = Console()
    status, content, status_code, _ = response
    color = _get_status_color(status_code)

    title = (
        f"""{description}: {status} [bold {color}]HTTP {status_code}[/bold {color}]"""
    )

    display_content = content
    if not extract_paths and isinstance(content, dict) and "buckets" in content:
        display_content = content["buckets"]

    if isinstance(display_content, (dict, list)):
        json_str = json.dumps(display_content, indent=4, ensure_ascii=False)
        body = Syntax(
            json_str,
            "json",
            theme="dracula",
            line_numbers=True,
            background_color="default",
        )
    else:
        body = str(display_content)

    console.print(Panel(body, title=title, border_style="blue", expand=True))
    console.print()

    is_success = status == "✅"
    _test_results[description] = (status, is_success, content)  # 保存content用于后续保存到文件

    if not extract_paths:
        return None
    if len(extract_paths) == 1:
        value = _deep_get(content, extract_paths[0])
        if value is None:
            console.print(
                f"[bold red]Warning:[/bold red] Could not extract '{extract_paths[0]}' from response."
            )
        return value
    values = []
    for path in extract_paths:
        v = _deep_get(content, path)
        if v is None:
            console.print(
                f"[bold red]Warning:[/bold red] Could not extract '{path}' from response."
            )
        values.append(v)
    return tuple(values)


def post(
    url: str,
    body: Optional[Union[str, Dict[str, Any], list]] = None,
    key: Optional[str] = None,
    should_fail: bool = False,
    extract: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
) -> Tuple[str, Any, int, Optional[str]]:
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if key:
        request_headers["Authorization"] = f"Bearer {key}"
    kwargs = {"headers": request_headers, "timeout": 10}
    if body is not None:
        if isinstance(body, (dict, list)):
            kwargs["data"] = json.dumps(body, ensure_ascii=False)
        else:
            kwargs["data"] = body
    try:
        resp = requests.post(url, **kwargs)
        status_code = resp.status_code
        if 200 <= status_code < 300:
            if should_fail:
                try:
                    details = resp.json()
                except ValueError:
                    details = resp.text
                return (
                    "❌",
                    {"error": f"期望失败但成功: {status_code}", "details": details},
                    status_code,
                    extract,
                )
            else:
                try:
                    return "✅", resp.json(), status_code, extract
                except ValueError:
                    return "✅", {"response": resp.text}, status_code, extract
        else:
            try:
                error_content = resp.json()
            except ValueError:
                error_content = resp.text

            if should_fail:
                return (
                    "✅",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
            else:
                return (
                    "❌",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
    except Exception as e:
        if should_fail:
            return "✅", {"error": "期望失败且成功", "details": str(e)}, 999, extract
        else:
            return "❌", {"error": "请求异常", "details": str(e)}, 999, extract


def delete(
    url: str,
    key: Optional[str] = None,
    should_fail: bool = False,
    extract: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
) -> Tuple[str, Any, int, Optional[str]]:
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if key:
        request_headers["Authorization"] = f"Bearer {key}"
    try:
        resp = requests.delete(url, headers=request_headers, timeout=10)
        status_code = resp.status_code
        if 200 <= status_code < 300:
            if should_fail:
                try:
                    details = resp.json()
                except ValueError:
                    details = resp.text
                return (
                    "❌",
                    {"error": f"期望失败但成功: {status_code}", "details": details},
                    status_code,
                    extract,
                )
            else:
                try:
                    return "✅", resp.json(), status_code, extract
                except ValueError:
                    return "✅", {"response": resp.text}, status_code, extract
        else:
            try:
                error_content = resp.json()
            except ValueError:
                error_content = resp.text

            if should_fail:
                return (
                    "✅",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
            else:
                return (
                    "❌",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
    except Exception as e:
        if should_fail:
            return "✅", {"error": "期望失败且成功", "details": str(e)}, 999, extract
        else:
            return "❌", {"error": "请求异常", "details": str(e)}, 999, extract


def put(
    url: str,
    body: Optional[Union[str, Dict[str, Any], list]] = None,
    key: Optional[str] = None,
    should_fail: bool = False,
    extract: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
) -> Tuple[str, Any, int, Optional[str]]:
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if key:
        request_headers["Authorization"] = f"Bearer {key}"
    kwargs = {"headers": request_headers, "timeout": 10}
    if body is not None:
        if isinstance(body, (dict, list)):
            kwargs["data"] = json.dumps(body, ensure_ascii=False)
        else:
            kwargs["data"] = body
    try:
        resp = requests.put(url, **kwargs)
        status_code = resp.status_code
        if 200 <= status_code < 300:
            if should_fail:
                try:
                    details = resp.json()
                except ValueError:
                    details = resp.text
                return (
                    "❌",
                    {"error": f"期望失败但成功: {status_code}", "details": details},
                    status_code,
                    extract,
                )
            else:
                try:
                    return "✅", resp.json(), status_code, extract
                except ValueError:
                    return "✅", {"response": resp.text}, status_code, extract
        else:
            try:
                error_content = resp.json()
            except ValueError:
                error_content = resp.text

            if should_fail:
                return (
                    "✅",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
            else:
                return (
                    "❌",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
    except Exception as e:
        if should_fail:
            return "✅", {"error": "期望失败且成功", "details": str(e)}, 999, extract
        else:
            return "❌", {"error": "请求异常", "details": str(e)}, 999, extract


def get(
    url: str,
    key: Optional[str] = None,
    should_fail: bool = False,
    extract: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
) -> Tuple[str, Any, int, Optional[str]]:
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if key:
        request_headers["Authorization"] = f"Bearer {key}"
    try:
        resp = requests.get(url, headers=request_headers, timeout=10)
        status_code = resp.status_code
        if not (200 <= status_code < 300):
            try:
                error_content = resp.json()
            except ValueError:
                error_content = resp.text

            if should_fail:
                return (
                    "✅",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
            else:
                return (
                    "❌",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
        try:
            json_data = resp.json()
        except ValueError:
            return (
                ("❌" if not should_fail else "✅"),
                "响应不是有效的JSON格式",
                status_code,
                extract,
            )

        if should_fail:
            return (
                "❌",
                {"error": "期望失败但成功", "details": json_data},
                status_code,
                extract,
            )
        else:
            return "✅", json_data, status_code, extract
    except Exception as e:
        if should_fail:
            return "✅", {"error": "期望失败且成功", "details": str(e)}, 999, extract
        else:
            return "❌", {"error": "请求异常", "details": str(e)}, 999, extract


def patch(
    url: str,
    body: Optional[Union[str, Dict[str, Any], list]] = None,
    key: Optional[str] = None,
    should_fail: bool = False,
    extract: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
) -> Tuple[str, Any, int, Optional[str]]:
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if key:
        request_headers["Authorization"] = f"Bearer {key}"

    kwargs = {"headers": request_headers, "timeout": 10}
    if body is not None:
        if isinstance(body, (dict, list)):
            kwargs["data"] = json.dumps(body, ensure_ascii=False)
        else:
            kwargs["data"] = body
    try:
        resp = requests.patch(url, **kwargs)
        status_code = resp.status_code

        if 200 <= status_code < 300:
            if should_fail:
                try:
                    details = resp.json()
                except ValueError:
                    details = resp.text
                return (
                    "❌",
                    {"error": f"期望失败但成功: {status_code}", "details": details},
                    status_code,
                    extract,
                )
            try:
                return "✅", resp.json(), status_code, extract
            except ValueError:
                return "✅", {"response": resp.text}, status_code, extract
        else:
            try:
                error_content = resp.json()
            except ValueError:
                error_content = resp.text

            if should_fail:
                return (
                    "✅",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
            return (
                "❌",
                {"error": f"状态码异常: {status_code}", "details": error_content},
                status_code,
                extract,
            )

    except Exception as e:
        if should_fail:
            return "✅", {"error": "期望失败且成功", "details": str(e)}, 999, extract
        else:
            return "❌", {"error": "请求异常", "details": str(e)}, 999, extract


def option(
    url: str,
    key: Optional[str] = None,
    should_fail: bool = False,
    extract: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
) -> Tuple[str, Any, int, Optional[str]]:
    request_headers = {"Content-Type": "application/json"}
    if headers:
        request_headers.update(headers)
    if key:
        request_headers["Authorization"] = f"Bearer {key}"

    try:
        resp = requests.options(url, headers=request_headers, timeout=10)
        status_code = resp.status_code

        if 200 <= status_code < 300:
            if should_fail:
                try:
                    details = resp.json()
                except ValueError:
                    details = {
                        "allow": resp.headers.get("Allow"),
                        "access_control_allow_methods": resp.headers.get(
                            "Access-Control-Allow-Methods"
                        ),
                        "access_control_allow_headers": resp.headers.get(
                            "Access-Control-Allow-Headers"
                        ),
                        "access_control_max_age": resp.headers.get(
                            "Access-Control-Max-Age"
                        ),
                    }
                return (
                    "❌",
                    {"error": f"期望失败但成功: {status_code}", "details": details},
                    status_code,
                    extract,
                )
            try:
                json_data = resp.json()
            except ValueError:
                json_data = {
                    "allow": resp.headers.get("Allow"),
                    "access_control_allow_methods": resp.headers.get(
                        "Access-Control-Allow-Methods"
                    ),
                    "access_control_allow_headers": resp.headers.get(
                        "Access-Control-Allow-Headers"
                    ),
                    "access_control_max_age": resp.headers.get(
                        "Access-Control-Max-Age"
                    ),
                }
            return "✅", json_data, status_code, extract
        else:
            try:
                error_content = resp.json()
            except ValueError:
                error_content = resp.text

            if should_fail:
                return (
                    "✅",
                    {"error": f"状态码异常: {status_code}", "details": error_content},
                    status_code,
                    extract,
                )
            return (
                "❌",
                {"error": f"状态码异常: {status_code}", "details": error_content},
                status_code,
                extract,
            )

    except Exception as e:
        if should_fail:
            return "✅", {"error": "期望失败且成功", "details": str(e)}, 999, extract
        else:
            return "❌", {"error": "请求异常", "details": str(e)}, 999, extract
