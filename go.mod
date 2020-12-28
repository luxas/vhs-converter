module github.com/luxas/digitized

go 1.15

replace github.com/weaveworks/libgitops => github.com/luxas/libgitops v0.0.4-0.20201228202736-69e47d6580cd

require (
	github.com/artdarek/go-unzip v1.0.0
	github.com/fluxcd/go-git-providers v0.0.2
	github.com/jinzhu/inflection v1.0.0
	github.com/labstack/echo v3.3.10+incompatible
	github.com/otiai10/copy v1.4.1
	github.com/sirupsen/logrus v1.7.0 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/weaveworks/libgitops v0.0.3
	k8s.io/apimachinery v0.18.6
)
