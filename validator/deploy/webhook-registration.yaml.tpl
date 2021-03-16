apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: codb-pod-validator-webhook
  labels:
    app: codb-pod-validator-webhook
    kind: validating
webhooks:
  - name: codb-pod-validator-webhook.slok.xyz
    clientConfig:
      service:
        name: codb-pod-validator-webhook
        namespace: ldy
        path: "/validating"
      caBundle: CA_BUNDLE
    rules:
      - operations: [ "DELETE" ]
        apiGroups: [""]
        apiVersions: ["v1"]
        resources: ["pods"]