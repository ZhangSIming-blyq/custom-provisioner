### 1. Dynamic Provisioner 例子与完整流程介绍

#### **原理概述**

在 Kubernetes 中，动态 provisioner 是一个实现了 `Provisioner` 接口的控制器，用于自动化存储卷的创建。当用户提交 PVC (PersistentVolumeClaim) 时，provisioner 根据定义的 StorageClass，自动创建相应的 PV (PersistentVolume)。这种自动化存储管理机制大大简化了卷的生命周期管理，减少了手动操作的复杂性。

#### **流程概述**
自定义动态 provisioner 的流程包括以下几个步骤：
1. 创建自定义的 Provisioner 逻辑，负责监听 PVC 的创建事件，并生成 PV。
2. 编写自定义的 StorageClass，使用该 Provisioner 动态创建卷。
3. 编写控制器代码，处理卷的创建与删除。
4. 部署自定义 provisioner 到 Kubernetes 集群中，并验证其功能。

#### **步骤 1: 自定义 Provisioner 代码实现**

https://github.com/ZhangSIming-blyq/custom-provisioner

首先，我们通过 Go 语言编写一个简单的自定义 provisioner，模拟卷的创建和删除过程。核心是自定义Provisioner结构体，实现Provision和Delete方法。Provision方法用于创建卷，Delete方法用于删除卷。

```go
package main

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"os"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v7/controller"
)

type customProvisioner struct {
	// Define any dependencies that your provisioner might need here, here I use the kubernetes client
	client kubernetes.Interface
}

// NewCustomProvisioner creates a new instance of the custom provisioner
func NewCustomProvisioner(client kubernetes.Interface) controller.Provisioner {
	// customProvisioner needs to implement "Provision" and "Delete" methods in order to satisfy the Provisioner interface
	return &customProvisioner{
		client: client,
	}
}

func (p *customProvisioner) Provision(options controller.ProvisionOptions) (*corev1.PersistentVolume, controller.ProvisioningState, error) {
	// Validate the PVC spec, 0 storage size is not allowed
	requestedStorage := options.PVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if requestedStorage.IsZero() {
		return nil, controller.ProvisioningFinished, fmt.Errorf("requested storage size is zero")
	}

	// If no access mode is specified, return an error
	if len(options.PVC.Spec.AccessModes) == 0 {
		return nil, controller.ProvisioningFinished, fmt.Errorf("access mode is not specified")
	}

	// Generate a unique name for the volume using the PVC namespace and name
	volumeName := fmt.Sprintf("pv-%s-%s", options.PVC.Namespace, options.PVC.Name)

	// Check if the volume already exists
	volumePath := "/tmp/dynamic-volumes/" + volumeName
	if _, err := os.Stat(volumePath); !os.IsNotExist(err) {
		return nil, controller.ProvisioningFinished, fmt.Errorf("volume %s already exists at %s", volumeName, volumePath)
	}

	// Create the volume directory
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf("failed to create volume directory: %v", err)
	}

	// Based on the above checks, we can now create the PV, HostPath is used as the volume source
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: volumeName,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: options.PVC.Spec.Resources.Requests[corev1.ResourceStorage],
			},
			AccessModes:                   options.PVC.Spec.AccessModes,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: volumePath,
				},
			},
		},
	}

	// Return the PV, ProvisioningFinished and nil error to indicate success
	klog.Infof("Successfully provisioned volume %s for PVC %s/%s", volumeName, options.PVC.Namespace, options.PVC.Name)
	return pv, controller.ProvisioningFinished, nil
}

func (p *customProvisioner) Delete(volume *corev1.PersistentVolume) error {
	// Validate whether the volume is a HostPath volume
	if volume.Spec.HostPath == nil {
		klog.Infof("Volume %s is not a HostPath volume, skipping deletion.", volume.Name)
		return nil
	}

	// Get the volume path
	volumePath := volume.Spec.HostPath.Path

	// Check if the volume path exists
	if _, err := os.Stat(volumePath); os.IsNotExist(err) {
		klog.Infof("Volume path %s does not exist, nothing to delete.", volumePath)
		return nil
	}

	// Delete the volume directory, using os.RemoveAll to delete the directory and its contents
	klog.Infof("Deleting volume %s at path %s", volume.Name, volumePath)
	if err := os.RemoveAll(volumePath); err != nil {
		klog.Errorf("Failed to delete volume %s at path %s: %v", volume.Name, volumePath, err)
		return err
	}

	klog.Infof("Successfully deleted volume %s at path %s", volume.Name, volumePath)
	return nil
}

func main() {
	// Use "InClusterConfig" to create a new clientset
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("Failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create clientset: %v", err)
	}

	provisioner := NewCustomProvisioner(clientset)

	// Important!! Create a new ProvisionController instance and run it
	pc := controller.NewProvisionController(clientset, "custom-provisioner", provisioner, controller.LeaderElection(false))
	klog.Infof("Starting custom provisioner...")
	pc.Run(context.Background())
}
```

#### **步骤 2: 部署自定义 Provisioner 到 Kubernetes**

**构建 Docker 镜像：**

首先，我们将上述代码打包成 Docker 镜像，以下是一个简单的 Dockerfile:

```Dockerfile
FROM golang:1.23 as builder
WORKDIR /workspace
COPY . .
WORKDIR /workspace/cmd/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o custom-provisioner .

FROM alpine:3.14
COPY --from=builder /workspace/cmd/custom-provisioner /custom-provisioner
ENTRYPOINT ["/custom-provisioner"]
```

**创建自定义 Provisioner 的 Deployment：**

这里因为我们要使用HostPath的tmp目录，所以需要在Deployment中挂载/tmp目录。

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-provisioner
spec:
  replicas: 1
  selector:
    matchLabels:
      app: custom-provisioner
  template:
    metadata:
      labels:
        app: custom-provisioner
    spec:
      containers:
        - name: custom-provisioner
          image: siming.net/sre/custom-provisioner:main_dc62f09_2024-09-29-010124
          imagePullPolicy: IfNotPresent
          env:
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: PROVISIONER_NAME
              value: custom-provisioner
          volumeMounts:
            - mountPath: /tmp
              name: tmp-dir
      volumes:
        - name: tmp-dir
          hostPath:
            path: /tmp
            type: Directory

```

#### **步骤 3: 创建 StorageClass**

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: custom-storage
provisioner: custom-provisioner
parameters:
  type: custom
```

#### **步骤 4: RBAC 权限配置**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-provisioner-role
rules:
  - apiGroups: [""]
    resources: ["persistentvolumes", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "create", "delete", "update"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]

---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: custom-provisioner-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: custom-provisioner-role
subjects:
  - kind: ServiceAccount
    name: default
    namespace: system
```

#### **步骤 5: 创建 PVC 来触发 Provisioner**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: custom-pvc
spec:
  storageClassName: custom-storage
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

当 PVC 被创建时，Kubernetes 将触发自定义 provisioner 来创建并绑定卷。

```bash
# 成功部署custom-provisioner, 并且pod本地的tmp目录作为存储目录
kp
NAME                                  READY   STATUS    RESTARTS   AGE
controller-manager-578b69d9d4-t228b   1/1     Running   0          11d
custom-provisioner-58d77856f9-4m4l8   1/1     Running   0          3m19s

# 创建pvc后查看日志
k logs -f custom-provisioner-58d77856f9-4m4l8
I0928 17:25:36.663192       1 main.go:121] Starting custom provisioner...
I0928 17:25:36.663264       1 controller.go:810] Starting provisioner controller custom-provisioner_custom-provisioner-58d77856f9-4m4l8_6328a9e1-cdeb-4088-b8e2-c83b140429e3!
I0928 17:25:36.764192       1 controller.go:859] Started provisioner controller custom-provisioner_custom-provisioner-58d77856f9-4m4l8_6328a9e1-cdeb-4088-b8e2-c83b140429e3!
I0928 17:25:56.495556       1 controller.go:1413] delete "pv-system-custom-pvc": started
I0928 17:25:56.495588       1 main.go:90] Volume path /tmp/dynamic-volumes/pv-system-custom-pvc does not exist, nothing to delete.
I0928 17:25:56.495598       1 controller.go:1428] delete "pv-system-custom-pvc": volume deleted
I0928 17:25:56.502579       1 controller.go:1478] delete "pv-system-custom-pvc": persistentvolume deleted
I0928 17:25:56.502604       1 controller.go:1483] delete "pv-system-custom-pvc": succeeded
I0928 17:26:13.279654       1 controller.go:1279] provision "system/custom-pvc" class "custom-storage": started
I0928 17:26:13.279822       1 main.go:74] Successfully provisioned volume pv-system-custom-pvc for PVC system/custom-pvc
I0928 17:26:13.279839       1 controller.go:1384] provision "system/custom-pvc" class "custom-storage": volume "pv-system-custom-pvc" provisioned
I0928 17:26:13.279855       1 controller.go:1397] provision "system/custom-pvc" class "custom-storage": succeeded
I0928 17:26:13.279891       1 volume_store.go:212] Trying to save persistentvolume "pv-system-custom-pvc"
I0928 17:26:13.280969       1 event.go:377] Event(v1.ObjectReference{Kind:"PersistentVolumeClaim", Namespace:"system", Name:"custom-pvc", UID:"8dc69c67-609b-48aa-b5d7-ae932f91a8d7", APIVersion:"v1", ResourceVersion:"3337346", FieldPath:""}): type: 'Normal' reason: 'Provisioning' External provisioner is provisioning volume for claim "system/custom-pvc"
I0928 17:26:13.297182       1 volume_store.go:219] persistentvolume "pv-system-custom-pvc" saved
I0928 17:26:13.297444       1 event.go:377] Event(v1.ObjectReference{Kind:"PersistentVolumeClaim", Namespace:"system", Name:"custom-pvc", UID:"8dc69c67-609b-48aa-b5d7-ae932f91a8d7", APIVersion:"v1", ResourceVersion:"3337346", FieldPath:""}): type: 'Normal' reason: 'ProvisioningSucceeded' Successfully provisioned volume pv-system-custom-pvc

# 查看pvc
k get pvc 
NAME         STATUS   VOLUME                 CAPACITY   ACCESS MODES   STORAGECLASS     AGE
custom-pvc   Bound    pv-system-custom-pvc   1Gi        RWO            custom-storage   2m51s

# 已经动态绑定好pv
k get pv
NAME                   CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM               STORAGECLASS     REASON   AGE
pv-system-custom-pvc   1Gi        RWO            Delete           Bound    system/custom-pvc   custom-storage            2m53s

# 目录也成功创建
ls /tmp/dynamic-volumes/  
pv-system-custom-pvc
```

### 2. StorageClass 所有字段及其功能介绍

#### **StorageClass 示例 YAML**

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: custom-storage
provisioner: example.com/custom-provisioner
parameters:
  type: fast
  zone: us-east-1
  replication-type: none
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
mountOptions:
  - discard
  - nobarrier
```

#### **字段解释及控制逻辑**

##### **1. `provisioner`**

- **功能**:
    - `provisioner` 字段指定了用于动态创建卷的 provisioner。**<font color=seagreen>它决定了 Kubernetes 如何与外部存储系统交互</font>**。不同的 provisioner 可以对接不同类型的存储系统（如 AWS EBS、GCE Persistent Disks、自定义的 provisioner）。

- **受控组件**:
    - `kube-controller-manager` 中的 PersistentVolume controller 负责根据该字段选择合适的 provisioner 实现，并向其发出创建卷的请求。该字段的值会与定义在集群中的 provisioner 匹配，例如 `example.com/custom-provisioner`。

##### **2. `parameters`**

- **功能**:
    - `parameters` 字段用于向 provisioner 传递自定义参数。这些参数可以根据存储系统的特性进行定制。例如，`type` 参数可以定义存储类型（如高性能或标准存储），`zone` 参数可以定义存储卷的区域，`replication-type` 可以定义数据是否有复制策略。

- **受控组件**:
    - **<font color=seagreen>由 provisioner 自身解析并使用这些参数</font>**，在创建 PV 时根据传递的参数设置存储卷的属性。例如，如果 provisioner 是自定义的，它需要解释 `parameters` 中的内容，并与存储系统交互以执行卷的创建。

##### **3. `reclaimPolicy`**

- **功能**:
    - 定义了当 PVC 被删除时，PV 的行为。选项包括：
        - `Retain`: 卷不会被删除，数据保留。
        - `Delete`: 卷会被删除，存储资源也会被释放。
        - <strike>`Recycle`: 卷会被擦除并返回到未绑定状态（在 Kubernetes 1.9 之后已废弃）。</strike>

- **受控组件**:
    - 由 `kube-controller-manager` 中的 PersistentVolume controller 管理。**<font color=seagreen>当 PVC 释放后，controller 根据 `reclaimPolicy` 执行相应的删除或保留操作。</font>**

##### **4. `volumeBindingMode`**

- **功能**:
    - 决定了 PVC 何时绑定到 PV。选项包括：
        - `Immediate`: PVC 提交时立即绑定到可用的 PV。
        - `WaitForFirstConsumer`: 仅在 Pod 被调度时才绑定 PV。这可以避免资源分配的不平衡问题，特别适用于多可用区环境下存储和计算资源的协同调度。先调度后绑定这种延迟绑定方式在pv有多种选择的时候，先根据pod的需求选择pv，避免调度冲突(pv调度和pod调度冲突)

- **受控组件**:
    - **<font color=seagreen>`kube-scheduler` 负责在 `WaitForFirstConsumer` 模式下，根据 Pod 调度的节点选择合适的 PV</font>**，并执行 PVC 的绑定。在 `Immediate` 模式下，PVC 和 PV 的绑定则由 `kube-controller-manager` 中的 PersistentVolume controller 处理。

##### **5. `allowVolumeExpansion`**

- **功能**:
    - 当该字段设置为 `true` 时，允许用户动态扩展已经绑定的卷的大小。如果 PVC 需要更多存储空间，可以通过修改 PVC 的规格来触发卷扩展。

- **受控组件**:
    - `kube-controller-manager` 中的 `ExpandController` 负责处理卷扩展请求。当 PVC 被修改以请求更大的存储容量时，该 controller 会相应地对底层存储执行扩展操作，**<font color=seagreen>具体依赖于 storage provider 是否支持卷扩展</font>**。

##### **6. `mountOptions`**

- **功能**:
    - 定义卷在挂载时的选项。例如，在上述示例中，`discard` 选项表示在卷删除时自动丢弃数据，`nobarrier` 选项用于提高写入性能。这些选项会影响卷的使用方式。

- **受控组件**:
    - `kubelet` 负责在节点上处理挂载卷的操作。**<font color=seagreen>`kubelet` 在实际挂载卷到 Pod 时，会使用 StorageClass 中定义的挂载选项。</font>**

### **字段与 Controller 交互表格总结**

| 字段                | 作用                                      | 受控的组件                           |
|--------------------|-----------------------------------------|--------------------------------------|
| `provisioner`      | 指定动态卷 provisioner 使用哪个驱动         | `kube-controller-manager` 中的 PersistentVolume controller |
| `parameters`       | 向 provisioner 提供自定义参数                | 由 provisioner 自身逻辑解析           |
| `reclaimPolicy`    | 卷删除后保留、回收或删除                   | `kube-controller-manager` 中的 PersistentVolume controller |
| `volumeBindingMode`| PVC 何时绑定 PV                          | `kube-scheduler` (等待消费者模式) 或 `kube-controller-manager` (立即绑定模式) |
| `allowVolumeExpansion` | 允许扩展卷                             | `ExpandController` 在 `kube-controller-manager` 中处理 |
| `mountOptions`     | 卷挂载时的选项                             | `kubelet` 负责挂载时的处理            |

### 3. 整体挂载流程介绍：Attach、Detach、Mount、Unmount

Kubernetes 中的存储卷挂载流程涉及两个主要组件：**ControllerManager** 和 **Kubelet**。每个阶段都有不同的控制器负责管理卷的挂载、卸载等操作。

#### 1. **Attach（由 ControllerManager 处理）**
- **概述**: 当一个 Pod 被调度到某个节点，并且该 Pod 需要使用 PersistentVolume（如 EBS 或 GCE Persistent Disk），`AttachDetachController` 会将卷附加（Attach）到该节点。
- **过程**:
    1. Pod 被调度到某个节点。
    2. `AttachDetachController` 通过 Kubernetes API 获取 PVC 的相关信息，找到相应的 PersistentVolume。
    3. 使用 Cloud Provider 或者 CSI 驱动将卷附加到节点。
    4. 一旦卷附加成功，卷的状态将更新为 “Attached”，Pod 可以继续进入挂载阶段。

#### 2. **Detach（由 ControllerManager 处理）**
- **概述**: 当 Pod 被删除或调度到另一个节点时，`AttachDetachController` 会触发卷的卸载过程，将卷从原节点分离。
- **过程**:
    1. 当 Pod 终止时，`AttachDetachController` 检查卷是否仍然附加在节点上。
    2. 如果卷不再使用，控制器会通过 Cloud Provider 或 CSI 驱动将卷从节点上分离。
    3. 卷状态更新为 “Detached”，资源释放。

#### 3. **Mount（由 Kubelet 处理）**
- **概述**: 当卷附加到节点后，`kubelet` 会将卷挂载到 Pod 的容器中，这个过程通过 `VolumeManager` 来管理。
- **过程**:
    1. `kubelet` 监控到卷已附加，准备进行挂载操作。
    2. `VolumeManager` 负责将卷挂载到宿主机的文件系统（如 `/var/lib/kubelet/pods/...` 路径）。
    3. 卷挂载完成后，卷可通过容器内的目录访问。

#### 4. **Unmount（由 Kubelet 处理）**
- **概述**: 当 Pod 删除时，`kubelet` 会将该卷从宿主机文件系统中卸载。
- **过程**:
    1. `VolumeManager` 检查到卷不再使用，准备卸载。
    2. `kubelet` 执行卸载操作，卷从宿主机文件系统中移除。
    3. 卸载完成后，卷资源释放，Pod 生命周期结束。

### Pod 磁盘挂载具体流程：
1. 用户创建 Pod 并指定 PVC。
2. **Attach**: `AttachDetachController` 将卷附加到节点。
3. **Mount**: `kubelet` 将卷挂载到节点，并将其映射到 Pod 的容器中。
4. **Unmount**: 当 Pod 终止时，`kubelet` 执行卷的卸载操作。
5. **Detach**: `AttachDetachController` 将卷从节点分离。
