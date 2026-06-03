package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed build/*
var buildFS embed.FS

//go:embed routes.json
var routesJSON []byte

type routeItem struct {
	Path string `json:"path"`
	File string `json:"file"`
}

type config struct {
	Port   int         `json:"port"`
	Routes []routeItem `json:"routes"`
}

func main() {
	var cfg config
	if err := json.Unmarshal(routesJSON, &cfg); err != nil {
		log.Fatalf("routes.json 格式错误: %v", err)
	}

	app := fiber.New()
	app.Use(compress.New())
	app.Use(etag.New())

	for _, r := range cfg.Routes {
		target := filepath.Join("build", r.File)
		app.Get(r.Path, func(c *fiber.Ctx) error {
			data, err := buildFS.ReadFile(target)
			if err != nil {
				return c.Status(404).SendString("File not found")
			}

			contentType := "text/html"
			if filepath.Ext(r.File) == ".css" {
				contentType = "text/css"
			} else if filepath.Ext(r.File) == ".js" {
				contentType = "application/javascript"
			} else if filepath.Ext(r.File) == ".json" {
				contentType = "application/json"
			}

			c.Set("Content-Type", contentType)
			return c.Send(data)
		})
	}

	app.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(buildFS),
		PathPrefix: "build",
		MaxAge:     86400,
	}))

	app.Use("*", func(c *fiber.Ctx) error {
		data, err := buildFS.ReadFile("build/index.html")
		if err != nil {
			return c.Status(404).SendString("Index file not found")
		}
		c.Set("Content-Type", "text/html")
		return c.Send(data)
	})

	port := cfg.Port

	go func() {
		time.Sleep(300 * time.Millisecond)
		openBrowser("http://127.0.0.1:3000")
	}()

	log.Fatal(app.Listen(fmt.Sprintf(":%d", port)))
}
