PLUGIN_NAME = angzam78/seaweedfs-volume-plugin
PLUGIN_TAG ?= latest
PLUGIN_ARCH = ???

ALL_ARCH = amd64 arm64

all: $(addprefix build-,$(ALL_ARCH))

build-%: clean rootfs-% create-%
	@echo Building $* ready.

distclean: clean rmbuilder $(addprefix disable-,$(ALL_ARCH)) $(addprefix remove-,$(ALL_ARCH))

clean: 
	rm -rf ./plugin

builder:
	docker buildx create --name crossplatform --platform linux/amd64,linux/arm64
	docker buildx inspect crossplatform --bootstrap
	docker buildx use crossplatform
	docker buildx ls

rmbuilder:
	docker buildx rm crossplatform || true

rootfs-%: PLATFORM = linux/$* 
rootfs-%: 
	docker rmi -f rootfsimage || true
	docker buildx build --load --platform ${PLATFORM} -t rootfsimage -f Dockerfile .
	docker create --platform ${PLATFORM} --name tmp rootfsimage
	mkdir -p ./plugin/rootfs/var/lib/docker-volumes
	docker export tmp | tar -x -C ./plugin/rootfs
	docker rm -vf tmp 
	cp config.json ./plugin

create-%: disable-% remove-%
	docker plugin create ${PLUGIN_NAME}:${PLUGIN_TAG}-$* ./plugin

enable-%:		
	docker plugin enable ${PLUGIN_NAME}:${PLUGIN_TAG}-$*

remove-%:
	docker plugin rm -f ${PLUGIN_NAME}:${PLUGIN_TAG}-$* || true

disable-%:
	docker plugin disable ${PLUGIN_NAME}:${PLUGIN_TAG}-$* || true

push: $(addprefix push-,$(ALL_ARCH)) 

push-%:  
	docker plugin push ${PLUGIN_NAME}:${PLUGIN_TAG}-$*

