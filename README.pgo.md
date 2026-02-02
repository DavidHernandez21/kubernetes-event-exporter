# PGO

Use [Profile-Guided Optimization (PGO)](https://go.dev/doc/pgo) to optimize the Go compiler itself is a multi-step process. This document outlines the steps required to build the Go compiler with PGO.

This can be eventually used in production.

## Steps to Build Go Compiler with PGO

1. Build the docker image with PGO support:

    **Note** Change the tag `kubernetes-event-exporter:1.8` to match the version you are working with.
    Also ensure you use the same tag in the deployment manifest `deploy/02-deployment.yaml`.

    ```bash
    docker buildx build -t kubernetes-event-exporter:1.8 .
    ```

2. Create kind cluster:

    ```bash
    kind create cluster
    ```

3. Load the docker image into the kind cluster:

    ```bash
    kind load docker-image kubernetes-event-exporter:1.8
    ```

4. Deploy the kubernetes-event-exporter with PGO enabled:

    **Note** You might need to create the `monitoring` namespace first if it does not exist.

    **Ideally, you should deploy the configuration and deployment that you have in your setup. For demonstration purposes, we will use the manifests in the `deploy/` directory.**

    ```bash
    kubectl apply --server-side -f deploy/
    ```

5. Generate load to create profiling data. For example by creating and deleting pods in a loop:

    ```bash
    for i in {1..1000}; do
      kubectl run pod-$i --image=nginx
      kubectl delete pod pod-$i
    done
    ```

6. After sufficient load has been generated, forward the pprof endpoint to your local machine:

    ```bash
    kubectl port-forward -n monitoring deployment/kubernetes-event-exporter 6060:6060
    ```

7. Collect and analyze the profiling data using `go tool pprof`:

    ```bash
    go tool pprof -http=:8080 http://127.0.0.1:6060/debug/pprof/profile?seconds=120
    ```

    This command will open a web interface on `http://127.0.0.1:8080` where you can analyze the profiling data.

    On the terminal you will see the path where the profile data is stored, e.g., `/tmp/profile123456789`.

8. Use the collected profile data to build the Go compiler with PGO:

    Copy/overwrite the profile to `default.pgo`:

    ```bash
    cp path/to/profile123456789 default.pgo
    ```

    Then (re)build the image as in step 1.


## Dynamically enable PGO in your application

You can also dynamically enable PGO in your Go applications by using the `ENABLE_PPROF` environment variable. for example using a `ConfigMap`:

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-env
  namespace: event-exporter
data:
  ENABLE_PPROF: "true"
```

Then mount this `ConfigMap` as environment variables in your deployment:

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: event-exporter
  namespace: event-exporter
spec:
    template:
      spec:
        containers:
        - name: event-exporter
          image: your-image:tag
          envFrom:
          - configMapRef:
              name: cm-env
              optional: true
```

And restart the deployment to apply the changes.
