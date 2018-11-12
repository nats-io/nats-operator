# TODO: arm64v8 is broken
archs = amd64 arm32v6 # arm64v8
operator = nats-operator
reloader = nats-server-config-reloader
tag = $(shell git describe --abbrev=0 --tags | sed 's/v//g')

amd64_args = --arch amd64
arm32v6_args = --arch arm --variant v6
# TODO: arm64v8 is broken
# arm64v8_args = --arch arm64 --variant v8

_ensure_repo:
	@test ! $(repo) &&\
	echo repo must be set. >&2 &&\
	exit 1 ||\
	true

.PHONY: operator
operator: _ensure_repo
	@- $(foreach a,$(archs), \
		sed 's/FROM /FROM $a\//g' docker/operator/Dockerfile > docker/operator/Dockerfile.$a; \
		docker build --tag $(repo)/$(operator):$a-$(tag) --file docker/operator/Dockerfile.$a .; \
	)

.PHONY: reloader
reloader: _ensure_repo
	@- $(foreach a,$(archs), \
		sed 's/FROM /FROM $a\//g' docker/reloader/Dockerfile > docker/reloader/Dockerfile.$a; \
		docker build --tag $(repo)/$(reloader):$a-$(tag) --file docker/reloader/Dockerfile.$a .; \
	)

.PHONY: push-operator
push-operator: operator
	@- $(foreach a,$(archs), \
		docker push $(repo)/$(operator):$a-$(tag); \
	)
	docker manifest create $(repo)/$(operator):$(tag) $(foreach a,$(archs), $(repo)/$(operator):$a-$(tag)) || \
		docker manifest create --amend $(repo)/$(operator):$(tag) $(foreach a,$(archs), $(repo)/$(operator):$a-$(tag))
	@- $(foreach a,$(archs), \
		docker manifest annotate \
			$(repo)/$(operator):$(tag) \
			$(repo)/$(operator):$a-$(tag) \
			--os linux $($a_args); \
	)
	docker manifest push $(repo)/$(operator):$(tag)

.PHONY: push-reloader
push-reloader: reloader
	@- $(foreach a,$(archs), \
		docker push $(repo)/$(reloader):$a-$(tag); \
	)
	docker manifest create $(repo)/$(reloader):$(tag) $(foreach a,$(archs), $(repo)/$(reloader):$a-$(tag)) || \
		docker manifest create --amend $(repo)/$(reloader):$(tag) $(foreach a,$(archs), $(repo)/$(reloader):$a-$(tag))
	@- $(foreach a,$(archs), \
		docker manifest annotate \
			$(repo)/$(reloader):$(tag) \
			$(repo)/$(reloader):$a-$(tag) \
			--os linux $($a_args); \
	)
	docker manifest push $(repo)/$(reloader):$(tag)
