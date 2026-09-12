variable "BUILD_VERSION" {
  default = "dev"
}

variable "BUILD_REVISION" {
  default = "unknown"
}

variable "IMAGE_PREFIX" {
  default = "self-deepsearch"
}

group "default" {
  targets = ["platform-api", "platform-worker", "display-web", "ops-web", "media-python"]
}

group "release-a" {
  targets = ["platform-api", "platform-worker", "display-web", "ops-web", "media-python"]
}

target "common" {
  context = "."
  args = {
    BUILD_VERSION  = BUILD_VERSION
    BUILD_REVISION = BUILD_REVISION
  }
}

target "platform-api" {
  inherits   = ["common"]
  dockerfile = "services/platform-api/Dockerfile"
  tags       = ["${IMAGE_PREFIX}/platform-api:${BUILD_VERSION}"]
}

target "platform-worker" {
  inherits   = ["common"]
  dockerfile = "services/platform-worker/Dockerfile"
  tags       = ["${IMAGE_PREFIX}/platform-worker:${BUILD_VERSION}"]
}

target "display-web" {
  inherits   = ["common"]
  dockerfile = "apps/display-web/Dockerfile"
  tags       = ["${IMAGE_PREFIX}/display-web:${BUILD_VERSION}"]
}

target "ops-web" {
  inherits   = ["common"]
  dockerfile = "apps/ops-web/Dockerfile"
  tags       = ["${IMAGE_PREFIX}/ops-web:${BUILD_VERSION}"]
}

target "media-python" {
  inherits   = ["common"]
  dockerfile = "workers/media-python/Dockerfile"
  tags       = ["${IMAGE_PREFIX}/media-python:${BUILD_VERSION}"]
}
