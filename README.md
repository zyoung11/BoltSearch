# BoltSearch

基于 **BoltDB** 的轻量级全文搜索引擎，内置 **BM25** 评分、**中英文分词**、**RESTful API**。

## 目录

- [原理](#原理)
- [安装](#安装)
- [CLI 使用](#cli-使用)
- [API 文档](#api-文档)
- [数据格式](#数据格式)

## 原理

### 存储结构

单文件 `search.db`，内部 6 个 Bucket：

| Bucket | Key | Value | 用途 |
|---|---|---|---|
| `docs` | DocID (uint64) | msgpack 文档 | 原始文档存储 |
| `index` | 词项 (string) | msgpack PostingList | 倒排索引 |
| `meta` | `"root"` | msgpack IndexMeta | 全局元数据 |
| `doclen` | DocID (uint64) | token 数 | 文档长度 |
| `df` | 词项 (string) | 文档频率 | IDF 计算 |
| `hash` | SHA256 前 8 字节 | DocID | 去重 |

### 分词流程

```
输入文本 → 语言检测 → gse 中文分词 / 空格拆分英文
        → 小写归一化 → snowball 词干提取(英文)
        → 停用词过滤 → 标点/符号过滤 → []Token
```

索引时和搜索时走**完全相同的分词流程**，保证一致性。

### 评分算法

**BM25**：

```
score(q,d) = Σ IDF(t) × (TF × (k1+1)) / (TF + k1×(1−b + b×|d|/avgdl))
```

- `k1=1.2` 控制 TF 饱和速度
- `b=0.75` 控制文档长度惩罚
- `IDF(t)` 来自 `df` Bucket，稀有词得分高

### 操作模型

所有操作基于 BoltDB 事务，**增量写入**，无需全量重建：

- 新增文档 → 单事务追加，不动已有数据
- 删除文档 → 单事务移除，其余索引不受影响
- 重复文档 → SHA256 哈希自动跳过

## 安装

### [下载 Release 二进制](https://github.com/zyoung11/BoltSearch/releases)

### 从源码编译

```bash
git clone https://github.com/zyoung11/BoltSearch
cd boltsearch
go build -ldflags="-s -w" .
```

## CLI 使用

### 初始化数据库

```bash
boltsearch init                    # 默认 search.db
boltsearch init --db mydata.db     # 指定路径
```

### 索引数据

```bash
boltsearch index data.jsonl
boltsearch index data.jsonl --db mydata.db
```

支持 **JSONL** 格式（每行一个 JSON 对象），自动去重：

```
[+] 索引完成，共 100 篇文档，耗时 45ms （新增 100 篇）
[+] 索引完成，共 0 篇文档，耗时 12ms （跳过 100 篇重复）
```

### 手动添加文档

```bash
boltsearch add --title "标题" --content "正文内容"
```

重复自动跳过。

### 搜索

```bash
boltsearch search "xxx"
boltsearch search "xxx" --mode and
boltsearch search "xxx" --prefix
boltsearch search "xxx" -n 20
```

### 删除

```bash
boltsearch delete 5
```

### 统计

```bash
boltsearch stats
```

输出：

```
  文档总数:     100
  Token总数:    6500
  平均文档长度: 65.0
  唯一词项数:   2400
  数据库大小:   512.0 KB
```

### 词项补全

```bash
boltsearch suggest xxx
boltsearch suggest pro -n 15
```

### 浏览数据库

```bash
boltsearch browse                  # Bucket 列表
boltsearch browse docs             # 查看文档（默认 20 条）
boltsearch browse index -n 50      # 查看倒排索引
```

### 启动 API 服务

```bash
boltsearch serve --addr 8080
```

### 通用选项

所有命令支持：

| 选项 | 说明 |
|---|---|
| `--db <path>` | 数据库路径（默认 `search.db`） |
| `-n, --limit <N>` | 返回条数 |
| `--format <fmt>` | 输出格式：`print` / `json` / `jsonl` / `csv` |

## API 文档

### 端点总览

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/index` | 上传 JSONL 文件 |
| `POST` | `/api/docs` | 添加单篇文档 |
| `GET` | `/api/search` | 搜索 |
| `GET` | `/api/docs/:id` | 获取文档 |
| `DELETE` | `/api/docs/:id` | 删除文档 |
| `GET` | `/api/stats` | 统计信息 |
| `GET` | `/api/suggest` | 词项补全 |
| `GET` | `/api/browse` | 浏览 Bucket |
| `GET` | `/api/browse?bucket=` | 浏览 Bucket 内容 |

---

### `POST /api/index`

上传 JSONL 文件并索引。

**请求**：`multipart/form-data`

| 字段 | 类型 | 说明 |
|---|---|---|
| `file` | file | JSONL 文件 |

**响应** `200`：

```json
{
  "indexed": 100,
  "summary": "（新增 95 篇，跳过 5 篇重复）"
}
```

---

### `POST /api/docs`

添加单篇文档。

**请求**：`application/json`

```json
{
  "title": "文档标题",
  "content": "文档正文"
}
```

**响应** `201`：

```json
{ "docID": 11 }
```

**响应** `409`（重复）：

```json
{ "error": "文档已存在", "duplicate": true }
```

---

### `GET /api/search`

全文搜索。

**参数**：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `q` | string | *必填* | 查询词 |
| `mode` | string | `or` | `or` / `and` |
| `limit` | int | `10` | 返回数量 |
| `offset` | int | `0` | 偏移量 |
| `prefix` | bool | `false` | 前缀匹配 |

**响应** `200`：

```json
{
  "query": "xxx",
  "mode": "or",
  "totalHits": 2,
  "results": [
    {
      "Doc": {
        "ID": 1,
        "Title": "xxxx",
        "Content": "xxxx..."
      },
      "Score": 2.24,
      "MatchedOn": ["xxx"],
      "MatchCount": 3
    }
  ]
}
```

---

### `GET /api/docs/:id`

获取文档详情。

**响应** `200`：

```json
{
  "docid": "1",
  "title": "xxx",
  "content": "xxxx..."
}
```

---

### `DELETE /api/docs/:id`

删除文档。

**响应** `200`：

```json
{ "deleted": 5 }
```

---

### `GET /api/stats`

数据库统计。

**响应** `200`：

```json
{
  "TotalDocs": 10,
  "TotalTokens": 649,
  "AvgDocLen": 64.9,
  "UniqueTerms": 394,
  "DBFileSize": 262144
}
```

---

### `GET /api/suggest`

词项自动补全。

**参数**：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `prefix` | string | *必填* | 前缀 |
| `limit` | int | `20` | 返回数量 |

**响应** `200`：

```json
["xx", "xxx", "xxxx"]
```

---

### `GET /api/browse`

浏览 Bucket 列表或 Bucket 内容。

**无参数** — 返回 Bucket 列表：

```json
[
  { "name": "docs",   "desc": "原始文档", "count": 10 },
  { "name": "index",  "desc": "倒排索引", "count": 394 }
]
```

**参数 `bucket=<name>`** — 返回 Bucket 内容：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `bucket` | string | — | Bucket 名称 |
| `limit` | int | `100` | 返回数量 |

```json
{
  "bucket": "docs",
  "total": 10,
  "rows": [
    { "docid": "1", "title": "...", "content": "..." }
  ]
}
```

## 数据格式

### JSONL 输入格式

每行一个独立的 JSON 对象，必须包含 `title` 和 `content`：

```jsonl
{"title": "文档标题", "content": "文档正文，中英文均可"}
{"title": "Another Doc", "content": "English content is also supported"}
```

- 空 `title` 且空 `content` 的行自动跳过
- 支持流式读取，百万行级别无内存压力
- 重复文档自动跳过（基于 Title + Content 的 SHA256）
