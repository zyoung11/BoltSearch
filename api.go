package main

import (
	"strconv"
	"strings"

	"boltsearch/engine"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func setupAPI(eng *engine.SearchEngine) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(cors.New())
	app.Use(logger.New())

	api := app.Group("/api")

	api.Post("/index", func(c *fiber.Ctx) error {
		file, err := c.FormFile("file")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "缺少文件: " + err.Error()})
		}
		f, err := file.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		defer f.Close()

		count, summary, err := eng.IndexFile(f)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"indexed": count,
			"summary": summary,
		})
	})

	api.Post("/docs", func(c *fiber.Ctx) error {
		var body struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "无效的 JSON"})
		}
		if body.Title == "" || body.Content == "" {
			return c.Status(400).JSON(fiber.Map{"error": "title 和 content 不能为空"})
		}
		docID, err := eng.AddDocument(body.Title, body.Content)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if docID == 0 {
			return c.Status(409).JSON(fiber.Map{"error": "文档已存在", "duplicate": true})
		}
		return c.Status(201).JSON(fiber.Map{"docID": docID})
	})

	api.Get("/search", func(c *fiber.Ctx) error {
		q := c.Query("q")
		if q == "" {
			return c.Status(400).JSON(fiber.Map{"error": "缺少查询参数 q"})
		}
		mode := c.Query("mode", "or")
		limit, _ := strconv.Atoi(c.Query("limit", "10"))
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		prefix := c.Query("prefix") == "true"

		results, total, err := eng.Search(q, mode, limit, offset, prefix)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{
			"query":     q,
			"mode":      mode,
			"totalHits": total,
			"results":   results,
		})
	})

	api.Delete("/docs/:id", func(c *fiber.Ctx) error {
		id, err := strconv.ParseUint(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "无效的文档 ID"})
		}
		if err := eng.DeleteDocument(id); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"deleted": id})
	})

	api.Get("/docs/:id", func(c *fiber.Ctx) error {
		id, err := strconv.ParseUint(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "无效的文档 ID"})
		}
		headers, rows, err := eng.ScanBucket("docs")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		for _, row := range rows {
			if docID, _ := strconv.ParseUint(row[0], 10, 64); docID == id {
				m := fiber.Map{}
				for i, h := range headers {
					if i < len(row) {
						m[strings.ToLower(h)] = row[i]
					}
				}
				return c.JSON(m)
			}
		}
		return c.Status(404).JSON(fiber.Map{"error": "文档不存在"})
	})

	api.Get("/stats", func(c *fiber.Ctx) error {
		stats, err := eng.Stats()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(stats)
	})

	api.Get("/suggest", func(c *fiber.Ctx) error {
		prefix := c.Query("prefix")
		if prefix == "" {
			return c.Status(400).JSON(fiber.Map{"error": "缺少查询参数 prefix"})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		suggestions, err := eng.Suggest(prefix, limit)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(suggestions)
	})

	api.Get("/browse", func(c *fiber.Ctx) error {
		bucket := c.Query("bucket")
		if bucket == "" {
			buckets := []string{"docs", "index", "meta", "doclen", "df", "hash"}
			descs := []string{"原始文档", "倒排索引", "全局元数据", "文档长度", "文档频率", "去重哈希"}
			counts := eng.BucketCounts()
			var list []fiber.Map
			for i, name := range buckets {
				list = append(list, fiber.Map{
					"name":  name,
					"desc":  descs[i],
					"count": counts[name],
				})
			}
			return c.JSON(list)
		}
		limit, _ := strconv.Atoi(c.Query("limit", "100"))
		headers, rows, err := eng.ScanBucket(bucket)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		total := len(rows)
		if limit > 0 && limit < len(rows) {
			rows = rows[:limit]
		}
		return c.JSON(fiber.Map{
			"bucket":  bucket,
			"total":   total,
			"headers": headers,
			"rows":    rows,
		})
	})

	return app
}
