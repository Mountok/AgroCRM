package handlers

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed swagger.yaml
var swaggerFS embed.FS

const swaggerUITemplate = `<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AgroCRM API — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    html { box-sizing: border-box; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: '/swagger/doc.yaml',
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis],
      layout: "BaseLayout",
      deepLinking: true,
      showExtensions: true,
      showCommonExtensions: true,
      defaultModelsExpandDepth: 1,
      defaultModelExpandDepth: 1,
    })
  </script>
</body>
</html>`

func swaggerUIHandler(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUITemplate))
}

func swaggerDocHandler(c *gin.Context) {
	data, err := swaggerFS.ReadFile("swagger.yaml")
	if err != nil {
		c.Data(http.StatusInternalServerError, "text/plain", []byte("swagger.yaml not found"))
		return
	}
	c.Data(http.StatusOK, "application/x-yaml; charset=utf-8", data)
}
