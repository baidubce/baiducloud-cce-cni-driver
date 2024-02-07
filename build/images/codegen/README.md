# images/codegen

This Docker image is a very lightweight image based on golang 1.13 which
includes codegen tools.

If you need to build a new version of the image and push it to Dockerhub, you
can run the following:

```bash
cd build/images/codegen
docker build -t hub.baidubce.com/jpaas-public/codegen:kubernetes-1.16.8 .
docker push hub.baidubce.com/jpaas-public/codegen:kubernetes-1.16.8
```

