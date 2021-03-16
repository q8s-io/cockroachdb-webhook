.PHONY: all


Docker: buildDocker pushDocker


buildDocker:
	@docker build -t uhub.service.ucloud.cn/infra/codbwebhook:v1 .

pushDocker:
	@docker push uhub.service.ucloud.cn/infra/codbwebhook:v1