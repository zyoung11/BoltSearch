package main

import (
	"boltsearch/engine"
	"boltsearch/logger"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

var log = logger.Logger{}

const (
	cLabel  = "\x1b[38;2;245;169;127m"
	cValKey = "\x1b[38;2;238;212;159m"
	cBold   = "\x1b[1m"
	cHdr    = "\x1b[38;2;198;160;246m"
	cApp    = "\x1b[38;2;138;173;244m"
	cCmd    = "\x1b[38;2;166;218;149m"
	cFlag   = "\x1b[38;2;238;212;159m"
	cRst    = "\x1b[0m"
)

func coloredPad(label string, width int, color string) string {
	visible := lipgloss.Width(label)
	padding := max(width-visible, 0)
	return color + label + strings.Repeat(" ", padding) + cRst
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "index":
		cmdIndex(args)
	case "search":
		cmdSearch(args)
	case "delete":
		cmdDelete(args)
	case "stats":
		cmdStats(args)
	case "suggest":
		cmdSuggest(args)
	case "browse":
		cmdBrowse(args)
	case "add":
		cmdAdd(args)
	case "init":
		cmdInit(args)
	case "serve":
		cmdServe(args)
	case "help", "-h", "--help":
		usage()
	default:
		log.Error("未知命令: " + cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	H := cBold + cHdr

	type line struct {
		name string
		args string
		desc string
	}
	lines := []line{
		{"index", "  <file.jsonl>", "索引 JSONL 文件"},
		{"search", " <查询词>", "搜索文档"},
		{"delete", " <docID>", "删除文档"},
		{"stats", "", "数据库统计"},
		{"suggest", "<前缀>", "词项自动补全"},
		{"init", "", "初始化空数据库"},
		{"add", "   " + cFlag + "--title T --content C" + cRst, "手动添加文档"},
		{"browse", " " + cFlag + "[<bucket>]" + cRst, "浏览数据库内容"},
		{"serve", " " + cFlag + " [--addr :8080]" + cRst, "启动 RESTful API 服务"},
	}

	maxArgs := 0
	for _, l := range lines {
		w := lipgloss.Width(l.name + " " + l.args)
		if w > maxArgs {
			maxArgs = w
		}
	}

	fmt.Print(H + "NAME:" + cRst + "\n" +
		"   " + cApp + "boltsearch - 基于 BoltDB 的全文搜索引擎" + cRst + "\n\n" +
		H + "USAGE:" + cRst + "\n")

	for _, l := range lines {
		cmdPart := l.name
		if l.args != "" {
			cmdPart += " " + l.args
		}
		pad := maxArgs - lipgloss.Width(cmdPart)
		if pad < 0 {
			pad = 0
		}
		fmt.Printf("   "+cApp+"boltsearch"+cRst+" "+cApp+"%s"+cRst+"%s "+cCmd+"%s"+cRst+"\n",
			cmdPart, strings.Repeat(" ", pad), l.desc)
	}

	fmt.Print("\n" + H + "OPTIONS:" + cRst + "\n" +
		"   " + cFlag + "--db <path>" + cRst + "        数据库路径 (默认: ./search.db)\n" +
		"   " + cFlag + "-n, --limit <N>" + cRst + "    返回条数 (默认: 10, browse 默认 20)\n" +
		"   " + cFlag + "--mode <and|or>" + cRst + "    搜索模式 (默认: or)\n" +
		"   " + cFlag + "--prefix" + cRst + "           启用前缀匹配\n" +
		"   " + cFlag + "--format <fmt>" + cRst + "     输出格式: print/json/jsonl/csv (默认: print)\n",
	)
}

type parsedArgs struct {
	positional []string
	dbPath     string
	limit      int
	mode       string
	prefix     bool
	format     string
	showHelp   bool
}

func parse(args []string) parsedArgs {
	pa := parsedArgs{
		dbPath: "search.db",
		limit:  10,
		mode:   "or",
	}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--db":
			if i+1 < len(args) {
				pa.dbPath = args[i+1]
				i += 2
			} else {
				i++
			}
		case "-n", "--limit":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil {
					pa.limit = v
				}
				i += 2
			} else {
				i++
			}
		case "--mode":
			if i+1 < len(args) {
				pa.mode = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--prefix":
			pa.prefix = true
			i++
		case "--format":
			if i+1 < len(args) {
				pa.format = args[i+1]
				i += 2
			} else {
				i++
			}
		case "-h", "--help":
			pa.showHelp = true
			i++
		default:
			pa.positional = append(pa.positional, args[i])
			i++
		}
	}
	return pa
}

func cmdIndex(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpIndex()
		return
	}

	if len(pa.positional) < 1 {
		log.Error("缺少输入文件路径")
		fmt.Println("用法: " + cApp + "boltsearch" + cRst + " " + cApp + "index" + cRst + " <file.jsonl> " + cFlag + "[--db <path>] [--format <fmt>]" + cRst)
		os.Exit(1)
	}

	filePath := pa.positional[0]
	f, err := os.Open(filePath)
	if err != nil {
		log.Error("无法打开文件: " + err.Error())
		os.Exit(1)
	}
	defer f.Close()

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	log.Info("正在索引 " + filepath.Base(filePath) + " ...")
	start := time.Now()

	count, summary, err := eng.IndexFile(f)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	elapsed := time.Since(start)
	msg := fmt.Sprintf("索引完成，共 %d 篇文档，耗时 %s", count, formatDuration(elapsed))
	if summary != "" {
		msg += " " + summary
	}
	log.Success(msg)
}

func cmdSearch(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpSearch()
		return
	}

	if len(pa.positional) < 1 {
		log.Error("缺少查询词")
		fmt.Println("用法: " + cApp + "boltsearch" + cRst + " " + cApp + "search" + cRst + " <查询词> " + cFlag + "[-n <N>] [--mode <and|or>] [--prefix] [--format <fmt>]" + cRst)
		os.Exit(1)
	}
	query := pa.positional[0]

	if pa.mode != "and" && pa.mode != "or" {
		log.Error("--mode 必须为 and 或 or")
		os.Exit(1)
	}

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	start := time.Now()
	results, totalHits, err := eng.Search(query, pa.mode, pa.limit, 0, pa.prefix)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	elapsed := time.Since(start)

	modeLabel := strings.ToUpper(pa.mode)
	if pa.prefix {
		modeLabel += " (前缀)"
	}

	switch pa.format {
	case "json":
		out, _ := json.MarshalIndent(map[string]any{
			"query":     query,
			"mode":      modeLabel,
			"totalHits": totalHits,
			"elapsed":   formatDuration(elapsed),
			"results":   results,
		}, "", "  ")
		fmt.Println(string(out))
		return
	case "csv":
		w := csv.NewWriter(os.Stdout)
		w.Write([]string{"Rank", "DocID", "Title", "Score", "Content"})
		for i, r := range results {
			w.Write([]string{
				strconv.Itoa(i + 1),
				strconv.FormatUint(r.Doc.ID, 10),
				r.Doc.Title,
				fmt.Sprintf("%.2f", math.Round(r.Score*100)/100),
				r.Doc.Content,
			})
		}
		w.Flush()
		return
	case "jsonl":
		for _, r := range results {
			line, _ := json.Marshal(r)
			fmt.Println(string(line))
		}
		return
	}

	if totalHits == 0 {
		log.Warn(fmt.Sprintf("未找到匹配结果 [模式: %s, 耗时: %s]", modeLabel, formatDuration(elapsed)))
		return
	}

	log.Success(fmt.Sprintf("找到 %d 条结果 [模式: %s, 耗时: %s]", totalHits, modeLabel, formatDuration(elapsed)))
	fmt.Println()

	for i, r := range results {
		content := r.Doc.Content
		runes := []rune(content)
		if len(runes) > 100 {
			content = string(runes[:100]) + "..."
		}
		fmt.Printf("  %2d. %-40s 得分: %.2f\n", i+1, truncate(r.Doc.Title, 40), math.Round(r.Score*100)/100)
		fmt.Printf("      %s\n", content)
		if i < len(results)-1 {
			fmt.Println()
		}
	}
}

func cmdDelete(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpDelete()
		return
	}

	if len(pa.positional) < 1 {
		log.Error("缺少文档 ID")
		fmt.Println("用法: " + cApp + "boltsearch" + cRst + " " + cApp + "delete" + cRst + " <docID> " + cFlag + "[--db <path>]" + cRst)
		os.Exit(1)
	}

	docID, err := strconv.ParseUint(pa.positional[0], 10, 64)
	if err != nil {
		log.Error("无效的文档 ID")
		os.Exit(1)
	}

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	if err := eng.DeleteDocument(docID); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	log.Success(fmt.Sprintf("文档 %d 已删除", docID))
}

func cmdStats(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpStats()
		return
	}

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	stats, err := eng.Stats()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	switch pa.format {
	case "json":
		out, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(out))
		return
	case "csv":
		w := csv.NewWriter(os.Stdout)
		w.Write([]string{"TotalDocs", "TotalTokens", "AvgDocLen", "UniqueTerms", "DBFileSize"})
		w.Write([]string{
			strconv.FormatUint(stats.TotalDocs, 10),
			strconv.FormatUint(stats.TotalTokens, 10),
			fmt.Sprintf("%.1f", stats.AvgDocLen),
			strconv.Itoa(stats.UniqueTerms),
			formatBytes(stats.DBFileSize),
		})
		w.Flush()
		return
	case "jsonl":
		line, _ := json.Marshal(stats)
		fmt.Println(string(line))
		return
	}

	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("文档总数:", 14, cLabel), fmt.Sprintf("%d", stats.TotalDocs))
	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("Token总数:", 14, cLabel), fmt.Sprintf("%d", stats.TotalTokens))
	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("平均文档长度:", 14, cLabel), fmt.Sprintf("%.1f", stats.AvgDocLen))
	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("唯一词项数:", 14, cLabel), fmt.Sprintf("%d", stats.UniqueTerms))
	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("数据库大小:", 14, cLabel), formatBytes(stats.DBFileSize))
}

func cmdSuggest(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpSuggest()
		return
	}

	if len(pa.positional) < 1 {
		log.Error("缺少前缀")
		fmt.Println("用法: " + cApp + "boltsearch" + cRst + " " + cApp + "suggest" + cRst + " <前缀> " + cFlag + "[-n <N>] [--format <fmt>]" + cRst)
		os.Exit(1)
	}

	prefix := pa.positional[0]

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	suggestions, err := eng.Suggest(prefix, pa.limit)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	if len(suggestions) == 0 {
		if pa.format == "json" {
			fmt.Println("[]")
		} else {
			log.Warn(fmt.Sprintf("未找到以 \"%s\" 开头的词项", prefix))
		}
		return
	}

	switch pa.format {
	case "json":
		out, _ := json.MarshalIndent(suggestions, "", "  ")
		fmt.Println(string(out))
		return
	case "csv":
		w := csv.NewWriter(os.Stdout)
		for _, s := range suggestions {
			w.Write([]string{s})
		}
		w.Flush()
		return
	case "jsonl":
		for _, s := range suggestions {
			line, _ := json.Marshal(s)
			fmt.Println(string(line))
		}
		return
	}

	log.Info(fmt.Sprintf("找到 %d 个匹配词项", len(suggestions)))
	for i, s := range suggestions {
		fmt.Printf("  %3d. %s\n", i+1, s)
	}
}

func cmdBrowse(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpBrowse()
		return
	}
	browseLimit := 20

	i := 0
	hasLimit := false
	for i < len(args) {
		if (args[i] == "-n" || args[i] == "--limit") && i+1 < len(args) {
			if v, err := strconv.Atoi(args[i+1]); err == nil {
				browseLimit = v
				hasLimit = true
			}
			i += 2
			continue
		}
		i++
	}
	if !hasLimit && pa.limit != 10 {
		browseLimit = pa.limit
	}

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	if len(pa.positional) == 0 {
		browseBuckets(eng, pa.dbPath, pa.format)
		return
	}

	browseBucket(eng, pa.positional[0], browseLimit, pa.format)
}

func browseBuckets(eng *engine.SearchEngine, dbPath, format string) {
	buckets := []string{"docs", "index", "meta", "doclen", "df", "hash"}
	descs := []string{"原始文档", "倒排索引", "全局元数据", "文档长度", "文档频率", "去重哈希"}
	counts := eng.BucketCounts()

	switch format {
	case "json":
		type b struct {
			Name  string `json:"name"`
			Desc  string `json:"desc"`
			Count int    `json:"count"`
		}
		var list []b
		for i, name := range buckets {
			list = append(list, b{name, descs[i], counts[name]})
		}
		out, _ := json.MarshalIndent(map[string]any{"db": dbPath, "buckets": list}, "", "  ")
		fmt.Println(string(out))
		return
	case "csv":
		w := csv.NewWriter(os.Stdout)
		w.Write([]string{"Name", "Desc", "Count"})
		for i, name := range buckets {
			w.Write([]string{name, descs[i], strconv.Itoa(counts[name])})
		}
		w.Flush()
		return
	case "jsonl":
		for i, name := range buckets {
			line, _ := json.Marshal(map[string]any{"name": name, "desc": descs[i], "count": counts[name]})
			fmt.Println(string(line))
		}
		return
	}

	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("数据库:", 12, cLabel), dbPath)
	fmt.Println()

	for i, name := range buckets {
		line := fmt.Sprintf("%s  %s%s", logger.InfoPrefix,
			coloredPad(name+":", 10, cValKey),
			fmt.Sprintf("%-8s  %d 条", descs[i], counts[name]))
		fmt.Println(line)
	}

	fmt.Println()
	fmt.Printf("  boltsearch browse <bucket> [-n N]  查看具体内容\n")
}

func browseBucket(eng *engine.SearchEngine, bucketName string, limit int, format string) {
	headers, rows, err := eng.ScanBucket(bucketName)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	total := len(rows)
	show := rows
	if limit > 0 && total > limit {
		show = rows[:limit]
	}

	switch format {
	case "json":
		type entry map[string]string
		var list []entry
		for _, row := range rows {
			e := make(entry)
			for j, h := range headers {
				if j < len(row) {
					e[h] = row[j]
				}
			}
			list = append(list, e)
		}
		out, _ := json.MarshalIndent(list, "", "  ")
		fmt.Println(string(out))
		return
	case "jsonl":
		for _, row := range rows {
			e := make(map[string]string)
			for j, h := range headers {
				if j < len(row) {
					e[h] = row[j]
				}
			}
			line, _ := json.Marshal(e)
			fmt.Println(string(line))
		}
		return
	case "csv":
		w := csv.NewWriter(os.Stdout)
		w.Write(headers)
		for _, row := range rows {
			w.Write(row)
		}
		w.Flush()
		return
	}

	fmt.Printf("%s %s%s\n", logger.InfoPrefix, coloredPad("Bucket:", 12, cLabel), bucketName)
	fmt.Printf("%s %s%d\n", logger.InfoPrefix, coloredPad("总数:", 12, cLabel), total)
	fmt.Println()

	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}
	for _, row := range show {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
			if colWidths[i] > 50 {
				colWidths[i] = 50
			}
		}
	}

	for i, h := range headers {
		fmt.Printf("  %-*s", colWidths[i], h)
		if i < len(headers)-1 {
			fmt.Print("  ")
		}
	}
	fmt.Println()
	for i := range colWidths {
		w := colWidths[i]
		fmt.Print("  " + strings.Repeat("─", w))
		if i < len(headers)-1 {
			fmt.Print("  ")
		}
	}
	fmt.Println()

	for _, row := range show {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				cell = string([]rune(cell)[:colWidths[i]-1]) + "…"
			}
			fmt.Printf("  %-*s", colWidths[i], cell)
			if i < len(row)-1 {
				fmt.Print("  ")
			}
		}
		fmt.Println()
	}

	if limit > 0 && total > limit {
		fmt.Printf("\n%s ...还有 %d 条 (用 -n %d 查看更多)\n", logger.InfoPrefix, total-limit, total)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s + strings.Repeat(" ", maxLen-len(runes))
	}
	return string(runes[:maxLen-1]) + "…"
}

func helpTable(title, desc string, usageLines, paramLines [][3]string) {
	fmt.Println(cBold + cHdr + title + cRst)
	fmt.Println()
	fmt.Println(desc)
	fmt.Println()

	type row struct {
		name string
		args string
		desc string
	}

	sections := [][2]any{{"用法:", usageLines}, {"参数:", paramLines}}
	for _, sec := range sections {
		lines := sec[1].([][3]string)
		if len(lines) == 0 {
			continue
		}
		fmt.Println(sec[0])
		fmt.Println()

		var rows []row
		for _, l := range lines {
			rows = append(rows, row{l[0], l[1], l[2]})
		}

		maxW := 0
		for _, r := range rows {
			w := lipgloss.Width(r.name + r.args)
			if w > maxW {
				maxW = w
			}
		}

		for _, r := range rows {
			cmdPart := r.name + r.args
			pad := maxW - lipgloss.Width(cmdPart)
			if pad < 0 {
				pad = 0
			}
			fmt.Printf("  "+cApp+"%s"+cRst+"%s%s "+cCmd+"%s"+cRst+"\n",
				cmdPart, strings.Repeat(" ", pad), "", r.desc)
		}
		fmt.Println()
	}
}

func helpIndex() {
	helpTable("boltsearch index",
		"索引 JSONL 文件到数据库，自动去重。",
		[][3]string{{"<file.jsonl>", "", "输入文件路径"}},
		[][3]string{
			{cFlag + "--db <path>" + cRst, "", "数据库路径 (默认: ./search.db)"},
			{cFlag + "--format <fmt>" + cRst, "", "输出格式: print/json/csv"},
		})
}

func helpSearch() {
	helpTable("boltsearch search",
		"全文搜索，支持 BM25 评分排序。",
		[][3]string{{"<查询词>", "", "搜索关键词"}},
		[][3]string{
			{cFlag + "-n, --limit <N>" + cRst, "", "返回结果数 (默认: 10)"},
			{cFlag + "--mode <and|or>" + cRst, "", "搜索模式 (默认: or)"},
			{cFlag + "--prefix" + cRst, "", "启用前缀匹配"},
			{cFlag + "--format <fmt>" + cRst, "", "输出格式: print/json/jsonl/csv"},
		})
}

func helpDelete() {
	helpTable("boltsearch delete",
		"删除指定文档，同时清理倒排索引。",
		[][3]string{{"<docID>", "", "文档 ID"}},
		[][3]string{
			{cFlag + "--db <path>" + cRst, "", "数据库路径 (默认: ./search.db)"},
		})
}

func helpStats() {
	helpTable("boltsearch stats",
		"显示数据库统计信息。",
		nil,
		[][3]string{
			{cFlag + "--db <path>" + cRst, "", "数据库路径 (默认: ./search.db)"},
			{cFlag + "--format <fmt>" + cRst, "", "输出格式: print/json/jsonl/csv"},
		})
}

func helpSuggest() {
	helpTable("boltsearch suggest",
		"词项自动补全，返回以指定前缀开头的索引词。",
		[][3]string{{"<前缀>", "", "搜索前缀"}},
		[][3]string{
			{cFlag + "-n, --limit <N>" + cRst, "", "返回数量 (默认: 20)"},
			{cFlag + "--format <fmt>" + cRst, "", "输出格式: print/json/jsonl/csv"},
		})
}

func helpBrowse() {
	helpTable("boltsearch browse",
		"浏览数据库内容。无参数列出所有 Bucket，指定 Bucket 名称查看内容。",
		[][3]string{{"" + cFlag + "[<bucket>]" + cRst, "", "Bucket 名称 (docs/index/meta/doclen/df/hash)"}},
		[][3]string{
			{cFlag + "-n, --limit <N>" + cRst, "", "返回条数 (默认: 20)"},
			{cFlag + "--format <fmt>" + cRst, "", "输出格式: print/json/jsonl/csv"},
		})
}

func helpInit() {
	helpTable("boltsearch init",
		"初始化空数据库，创建所需 Bucket。",
		nil,
		[][3]string{
			{cFlag + "--db <path>" + cRst, "", "数据库路径 (默认: ./search.db)"},
		})
}

func helpAdd() {
	helpTable("boltsearch add",
		"手动添加一篇文档，重复自动跳过。",
		[][3]string{
			{cFlag + "--title <text>" + cRst, "", "文档标题"},
			{cFlag + "--content <text>" + cRst, "", "文档正文"},
			{cFlag + "--db <path>" + cRst, "", "数据库路径 (默认: ./search.db)"},
		},
		nil)
}

func helpServe() {
	helpTable("boltsearch serve",
		"启动 RESTful API 服务。",
		nil,
		[][3]string{
			{cFlag + "--addr <addr>" + cRst, "", "监听地址 (默认: :8080)"},
			{cFlag + "--db <path>" + cRst, "", "数据库路径 (默认: ./search.db)"},
		})
}

func cmdInit(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpInit()
		return
	}
	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	eng.Close()
	log.Success(fmt.Sprintf("数据库已初始化: %s", pa.dbPath))
}

func cmdAdd(args []string) {
	title := ""
	content := ""
	dbPath := "search.db"

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-h", "--help":
			helpAdd()
			return
		case "--db":
			if i+1 < len(args) {
				dbPath = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--title":
			if i+1 < len(args) {
				title = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--content":
			if i+1 < len(args) {
				content = args[i+1]
				i += 2
			} else {
				i++
			}
		default:
			i++
		}
	}

	if title == "" || content == "" {
		log.Error("缺少 --title 或 --content")
		fmt.Println("用法: " + cApp + "boltsearch" + cRst + " " + cApp + "add" + cRst + " " + cFlag + "--title" + cRst + " \"标题\" " + cFlag + "--content" + cRst + " \"正文\" " + cFlag + "[--db <path>]" + cRst)
		os.Exit(1)
	}

	eng, err := engine.NewSearchEngine(dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	docID, err := eng.AddDocument(title, content)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	if docID == 0 {
		log.Warn("文档已存在，跳过")
	} else {
		log.Success(fmt.Sprintf("文档已添加，DocID=%d", docID))
	}
}

func cmdServe(args []string) {
	pa := parse(args)
	if pa.showHelp {
		helpServe()
		return
	}
	addr := ":8080"

	i := 0
	for i < len(args) {
		if args[i] == "--addr" && i+1 < len(args) {
			addr = args[i+1]
			i += 2
			continue
		}
		i++
	}

	log.Info("正在初始化搜索引擎...")
	start := time.Now()

	eng, err := engine.NewSearchEngine(pa.dbPath)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer eng.Close()

	log.Success(fmt.Sprintf("初始化完成，耗时 %s", formatDuration(time.Since(start))))

	app := setupAPI(eng)

	log.Info(fmt.Sprintf("API 服务启动: http://localhost%s", addr))
	log.Info("端点:")
	fmt.Println("  POST   /api/index         上传 JSONL 文件 (multipart file)")
	fmt.Println("  GET    /api/search?q=      搜索")
	fmt.Println("  GET    /api/docs/:id       获取文档")
	fmt.Println("  DELETE /api/docs/:id       删除文档")
	fmt.Println("  GET    /api/stats          统计")
	fmt.Println("  GET    /api/suggest?prefix= 自动补全")
	fmt.Println("  GET    /api/browse         浏览 Bucket 列表")
	fmt.Println("  GET    /api/browse?bucket=  浏览 Bucket 内容")
	fmt.Println()

	if err := app.Listen(addr); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}
