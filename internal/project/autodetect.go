package project

import (
	"fmt"
	"strings"
)

// detectedRuntime holds the detected language/framework and a ready-to-use Dockerfile.
type detectedRuntime struct {
	Name       string // e.g. "nextjs", "vite", "python-fastapi", "go"
	Dockerfile string
	// Port the container listens on (informational, used for display)
	Port int
}

// autoDetectRuntime inspects the cloned repo on the remote server and returns
// a best-guess Dockerfile. Returns nil when detection is inconclusive.
func (d *Deployer) autoDetectRuntime(dir string) (*detectedRuntime, error) {
	has := func(file string) bool {
		out := d.exec.RunQuiet(fmt.Sprintf("test -f %s/%s && echo yes || echo no", dir, file))
		return strings.TrimSpace(out) == "yes"
	}
	read := func(file string) string {
		return d.exec.RunQuiet(fmt.Sprintf("cat %s/%s 2>/dev/null | head -c 8192", dir, file))
	}

	switch {
	case has("package.json"):
		return detectNodeRuntime(read("package.json")), nil
	case has("requirements.txt") || has("pyproject.toml") || has("Pipfile"):
		reqs := read("requirements.txt")
		if reqs == "" {
			reqs = read("pyproject.toml")
		}
		return detectPythonRuntime(reqs), nil
	case has("go.mod"):
		return &detectedRuntime{Name: "go", Port: 8080, Dockerfile: dockerfileGo()}, nil
	case has("Gemfile"):
		return &detectedRuntime{Name: "ruby", Port: 3000, Dockerfile: dockerfileRuby()}, nil
	case has("composer.json"):
		return &detectedRuntime{Name: "php", Port: 80, Dockerfile: dockerfilePHP()}, nil
	case has("index.html"):
		return &detectedRuntime{Name: "static", Port: 80, Dockerfile: dockerfileStatic()}, nil
	}
	return nil, nil
}

func detectNodeRuntime(pkgJSON string) *detectedRuntime {
	p := strings.ToLower(pkgJSON)
	switch {
	case strings.Contains(p, `"next"`):
		return &detectedRuntime{Name: "nextjs", Port: 3000, Dockerfile: dockerfileNext()}
	case strings.Contains(p, `"@remix-run`):
		return &detectedRuntime{Name: "remix", Port: 3000, Dockerfile: dockerfileRemix()}
	case strings.Contains(p, `"nuxt"`):
		return &detectedRuntime{Name: "nuxt", Port: 3000, Dockerfile: dockerfileNuxt()}
	case strings.Contains(p, `"@sveltejs/kit"`):
		return &detectedRuntime{Name: "sveltekit", Port: 3000, Dockerfile: dockerfileNodeBuild("build", 3000)}
	case strings.Contains(p, `"vite"`):
		return &detectedRuntime{Name: "vite", Port: 80, Dockerfile: dockerfileViteStatic("dist")}
	case strings.Contains(p, `"react-scripts"`):
		return &detectedRuntime{Name: "cra", Port: 80, Dockerfile: dockerfileViteStatic("build")}
	case strings.Contains(p, `"express"`), strings.Contains(p, `"fastify"`), strings.Contains(p, `"koa"`):
		return &detectedRuntime{Name: "node-server", Port: 3000, Dockerfile: dockerfileNode(3000)}
	default:
		return &detectedRuntime{Name: "node", Port: 3000, Dockerfile: dockerfileNode(3000)}
	}
}

func detectPythonRuntime(reqs string) *detectedRuntime {
	r := strings.ToLower(reqs)
	switch {
	case strings.Contains(r, "fastapi") || strings.Contains(r, "uvicorn"):
		return &detectedRuntime{Name: "python-fastapi", Port: 8000, Dockerfile: dockerfileFastAPI()}
	case strings.Contains(r, "django"):
		return &detectedRuntime{Name: "python-django", Port: 8000, Dockerfile: dockerfileDjango()}
	case strings.Contains(r, "flask"):
		return &detectedRuntime{Name: "python-flask", Port: 5000, Dockerfile: dockerfileFlask()}
	case strings.Contains(r, "streamlit"):
		return &detectedRuntime{Name: "python-streamlit", Port: 8501, Dockerfile: dockerfileStreamlit()}
	default:
		return &detectedRuntime{Name: "python", Port: 8000, Dockerfile: dockerfilePython()}
	}
}

// ── Dockerfile templates ──────────────────────────────────────────────────────

func dockerfileNext() string {
	return `FROM node:20-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci

FROM node:20-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/public ./public
COPY --from=builder /app/package*.json ./
COPY --from=deps /app/node_modules ./node_modules
EXPOSE 3000
CMD ["npx", "next", "start"]
`
}

func dockerfileRemix() string {
	return `FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder /app/build ./build
COPY --from=builder /app/public ./public
COPY --from=builder /app/package*.json ./
RUN npm ci --omit=dev
EXPOSE 3000
CMD ["npm", "start"]
`
}

func dockerfileNuxt() string {
	return `FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV HOST=0.0.0.0
COPY --from=builder /app/.output ./.output
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
`
}

// dockerfileViteStatic builds with npm and serves with nginx.
// outDir is "dist" for Vite or "build" for CRA.
func dockerfileViteStatic(outDir string) string {
	return fmt.Sprintf(`FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/%s /usr/share/nginx/html
RUN printf 'server{listen 80;root /usr/share/nginx/html;index index.html;location / {try_files $uri $uri/ /index.html;}}\n' \
    > /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
`, outDir)
}

// dockerfileNodeBuild builds and runs a generic node server.
func dockerfileNodeBuild(buildOut string, port int) string {
	return fmt.Sprintf(`FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder /app/%s ./%s
COPY --from=builder /app/package*.json ./
RUN npm ci --omit=dev
EXPOSE %d
CMD ["node", "."]
`, buildOut, buildOut, port)
}

func dockerfileNode(port int) string {
	return fmt.Sprintf(`FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
EXPOSE %d
CMD ["node", "."]
`, port)
}

func dockerfileFastAPI() string {
	return `FROM python:3.12-slim
WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
`
}

func dockerflaskEntrypoint() string {
	// prefer app.py, fallback to main.py
	return "app.py"
}

func dockerfileFlask() string {
	return `FROM python:3.12-slim
WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 5000
ENV FLASK_APP=app.py
CMD ["python", "-m", "flask", "run", "--host=0.0.0.0", "--port=5000"]
`
}

func dockerfileDjango() string {
	return `FROM python:3.12-slim
WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
RUN python manage.py collectstatic --noinput 2>/dev/null || true
EXPOSE 8000
CMD ["python", "manage.py", "runserver", "0.0.0.0:8000"]
`
}

func dockerfileStreamlit() string {
	return `FROM python:3.12-slim
WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8501
CMD ["streamlit", "run", "app.py", "--server.address=0.0.0.0", "--server.port=8501"]
`
}

func dockerfilePython() string {
	return `FROM python:3.12-slim
WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || true
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
`
}

func dockerfileGo() string {
	return `FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /app/server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/server ./server
EXPOSE 8080
CMD ["./server"]
`
}

func dockerfileRuby() string {
	return `FROM ruby:3.3-slim
WORKDIR /app
COPY Gemfile* ./
RUN bundle install --without development test
COPY . .
EXPOSE 3000
CMD ["ruby", "app.rb"]
`
}

func dockerfilePHP() string {
	return `FROM php:8.3-apache
WORKDIR /var/www/html
COPY . .
RUN if [ -f composer.json ]; then \
    curl -sS https://getcomposer.org/installer | php -- --install-dir=/usr/local/bin --filename=composer && \
    composer install --no-dev --optimize-autoloader --no-interaction; fi
EXPOSE 80
`
}

func dockerfileStatic() string {
	return `FROM nginx:alpine
COPY . /usr/share/nginx/html
RUN printf 'server{listen 80;root /usr/share/nginx/html;index index.html;location / {try_files $uri $uri/ /index.html;}}\n' \
    > /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
`
}
