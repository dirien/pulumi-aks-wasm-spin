# How to create WASI node pools for AKS to run WASM Spin applications with Pulumi

## Introduction

In this blog post, I am going to show you, how to run a Fermyon Spin application on a WASI node pool all with Pulumi.

Recently Microsoft announced the that they move aways from `Krustlet` and instead use for their WASI node pools now
[containerd shims](https://github.com/deislabs/containerd-wasm-shims) to run WASM workloads.

The containerd shims are using [runwasi](https://github.com/deislabs/runwasi) as a
libary. [runwasi](https://github.com/deislabs/runwasi) is a project that aims to run wasm workloads running on Wasmtime,
a fast and secure runtime for WebAssembly, which is managed by containerd.

## What is WASM?

WebAssembly (WASM) is a binary format that is designed for maximum execution speed and portability using a WASM runtime.
The WASM runtime is designed to run on a target architecture, and execute WebAssemblies in a sandboxed environment to
ensure security at near-native speed. The WebAssembly System Interface (WASI) standardizes the interface between the
WASM runtime and the host system to provide access to system resources such as the file system or network.

## What is Fermyon Spin?

Spin is a framework for building cloud-native applications with WebAssembly components. It is created by Fermyon and is
fully open source. You can find the source code on [GitHub](https://github.com/fermyon/spin).

## Prerequisites

- [Pulumi](https://www.pulumi.com/docs/get-started/install/)
- [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- Rust installed (see [here](https://www.rust-lang.org/tools/install))
- Go installed (see [here](https://golang.org/doc/install))
- IDE of your choice (VS Code, IntelliJ, etc.)

## Enalbe AKS preview features

To install the `aks-preview` extension for Azure CLI, run the following command:

```bash
az extension add --name aks-preview
```

or update the `aks-preview` extension to the latest version:

```bash
az extension update --name aks-preview
```

## Register the `WasmNodePoolPreview` feature

You may need to register the `WasmNodePoolPreview` feature for your subscription by simply running the following
command:

```bash
az feature register --namespace "Microsoft.ContainerService" --name "WasmNodePoolPreview"
```

This will take a few minutes to complete. You can check the status of the feature registration by running the following
command:

```bash
az feature show --namespace "Microsoft.ContainerService" --name "WasmNodePoolPreview"
```

Once the `state` property of the feature is `Registered`, you can create a WASI node pool for your AKS cluster.

## Create your AKS cluster

Let's start a new pulumi project with the following command, using the `pulumi-azure-native` provider and `Go` as the
language of choice:

```bash
mkdir pulumi-aks-wasm-spin && cd pulumi-aks-wasm-spin
pulumi new azure-go --force
```

You will be asked to provide some information about your project. You can use the default values for all questions.

Now we can create our AKS cluster with the following code:

```go
package main

import (
	"encoding/base64"

	containerservice "github.com/pulumi/pulumi-azure-native-sdk/containerservice/v20230101"
	resources "github.com/pulumi/pulumi-azure-native-sdk/resources/v20220901"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create an Azure Resource Group
		resourceGroup, err := resources.NewResourceGroup(ctx, "wasm-aks-rg", &resources.ResourceGroupArgs{
			ResourceGroupName: pulumi.String("wasm-aks-rg"),
		})
		if err != nil {
			return err
		}

		wasmCluster, err := containerservice.NewManagedCluster(ctx, "wasm-aks-cluster", &containerservice.ManagedClusterArgs{
			ResourceGroupName: resourceGroup.Name,
			KubernetesVersion: pulumi.String("1.25.5"),
			ResourceName:      pulumi.String("wasm-aks-cluster"),
			Identity: &containerservice.ManagedClusterIdentityArgs{
				Type: containerservice.ResourceIdentityTypeSystemAssigned,
			},
			DnsPrefix: pulumi.String("wasm-aks-cluster"),
			AgentPoolProfiles: containerservice.ManagedClusterAgentPoolProfileArray{
				&containerservice.ManagedClusterAgentPoolProfileArgs{
					Name:         pulumi.String("agentpool"),
					Mode:         pulumi.String("System"),
					OsDiskSizeGB: pulumi.Int(30),
					OsType:       pulumi.String("Linux"),
					Count:        pulumi.Int(1),
					VmSize:       pulumi.String("Standard_B4ms"),
				},
			},
		})
		if err != nil {
			return err
		}

		wasmPool, err := containerservice.NewAgentPool(ctx, "wasm-aks-agentpool", &containerservice.AgentPoolArgs{
			AgentPoolName:     pulumi.String("wasmpool"),
			ResourceGroupName: resourceGroup.Name,
			ResourceName:      wasmCluster.Name,
			WorkloadRuntime:   pulumi.String("WasmWasi"),
			Count:             pulumi.Int(1),
			VmSize:            pulumi.String("Standard_B4ms"),
			OsType:            pulumi.String("Linux"),
		})
		if err != nil {
			return err
		}

		kubeconfig := pulumi.All(wasmCluster.Name, resourceGroup.Name).ApplyT(func(args []interface{}) (*string, error) {
			clusterName := args[0].(string)
			resourceGroupName := args[1].(string)
			creds, err := containerservice.ListManagedClusterUserCredentials(ctx, &containerservice.ListManagedClusterUserCredentialsArgs{
				ResourceGroupName: resourceGroupName,
				ResourceName:      clusterName,
			})
			if err != nil {
				return nil, err
			}
			decoded, err := base64.StdEncoding.DecodeString(creds.Kubeconfigs[0].Value)
			if err != nil {
				return nil, err
			}
			return pulumi.StringRef(string(decoded)), nil
		}).(pulumi.StringPtrOutput)

		ctx.Export("resourceGroupName", resourceGroup.Name)
		ctx.Export("wasmClusterName", wasmCluster.Name)
		ctx.Export("wasmAgentPoolName", wasmPool.Name)
		ctx.Export("kubeconfig", pulumi.ToSecret(kubeconfig))

		return nil
	})
}
```

The code above will create a very default AKS cluster with a single node pool. But then we will add a WASI node pool
using the `containerservice.NewAgentPool` function. It is very important that the `WorkloadRuntime` property is set
to `WasmWasi` to enable the `containerd-wasm-shims` on the node pool. Also, the `OsType` property must be set to `Linux`
and the `WASM/WASI` node pool can't be used as system node pool.

The last part of the code above is exporting the `kubeconfig` of the cluster. This is needed to connect to the cluster
if you want to use the `kubectl` CLI or `k9s` to inspect the cluster.

The last `ctx.Export` statement is exporting the `kubeconfig` as a secret and some other information about the
infrastructure. You can run the following command to see the exported values:

```bash
pulumi stack output
```

Before I continue with the next steps in regards to the infrastructure, I think it is a good time to use `Spin` to
create the workload for our WASI node pool.

## Build an application using Fermyon Spin

### Install the Spin CLI

Before we can start to build our application, we need to install the Spin CLI. To do so, we can run the following
command:

```bash
curl -fsSL https://developer.fermyon.com/downloads/install.sh | bash
sudo mv spin /usr/local/bin
```

There are more installation options available on the [Install Spin](https://developer.fermyon.com/spin/install)
documentation page depending on your operating system. I am using macOS, so I will use the above command.

### Create a new Spin project

Now with the Spin CLI installed, we can start to create our project. Spin supports multiple languages, but not all
features are available in every language. Please check
the [Language Support](https://developer.fermyon.com/spin/language-support-overview) page for more information.

In this blog post, we will be using the Rust language. First I will install the Spin Rust template to speed up the
creation of new applications.

> Note: You do not need templates to create a Spin application, but they help you to get started quickly.

```bash
spin templates install --git https://github.com/fermyon/spin --update
Copying remote template source
Installing template redis-rust...
Installing template static-fileserver...
Installing template http-grain...
Installing template http-swift...
Installing template http-php...
Installing template http-c...
Installing template redirect...
Installing template http-rust...
Installing template http-go...
Installing template http-zig...
Installing template http-empty...
Installing template redis-go...
Installed 12 template(s)

+------------------------------------------------------------------------+
| Name                Description                                        |
+========================================================================+
| http-c              HTTP request handler using C and the Zig toolchain |
| http-empty          HTTP application with no components                |
| http-go             HTTP request handler using (Tiny)Go                |
| http-grain          HTTP request handler using Grain                   |
| http-php            HTTP request handler using PHP                     |
| http-rust           HTTP request handler using Rust                    |
| http-swift          HTTP request handler using SwiftWasm               |
| http-zig            HTTP request handler using Zig                     |
| redirect            Redirects a HTTP route                             |
| redis-go            Redis message handler using (Tiny)Go               |
| redis-rust          Redis message handler using Rust                   |
| static-fileserver   Serves static files from an asset directory        |
+------------------------------------------------------------------------+
```

To build in Rust Spin components, we need to install the `wasm32-wasi` target for Rust. To install the target, run
following command:

```bash
rustup target add wasm32-wasi
```

Now we can call the `spin new` command to create a new Spin application:

```bash
spin new http-rust 
Enter a name for your new application: aks-spin-demo
Description: Demo Spin application for AKS WASI node pool
HTTP base: /
HTTP path: /api/figlet
```

This should generate all the files we need and a directory called `aks-spin-demo`. The `spin.toml` file contains the
configuration for the application. Let's
take a look at the `spin.toml` file:

```toml
spin_manifest_version = "1"
authors = ["Engin Diri"]
description = "Demo Spin application for AKS WASI node pool"
name = "aks-spin-demo"
trigger = { type = "http", base = "/" }
version = "0.1.0"

[[component]]
id = "aks-spin-demo"
source = "target/wasm32-wasi/release/aks_spin_demo.wasm"
allowed_http_hosts = []
[component.trigger]
route = "/api/figlet"
[component.build]
command = "cargo build --target wasm32-wasi --release"
```

As we want to build a nice Figlet application, we need to add following dependencies to the `Cargo.toml` file:

```toml
figlet-rs = "0.1.5"
```

We then need to change the code in the `src/lib.rs` file to the following:

```rust
use anyhow::Result;
use spin_sdk::{
    http::{Request, Response},
    http_component,
};
use figlet_rs::FIGfont;

/// A simple Spin HTTP component.
#[http_component]
fn handle_aks_spin_demo(_: Request) -> Result<Response> {
    let standard_font = FIGfont::standard().unwrap();
    let figure = standard_font.convert("Hello, Fermyon on Azure AKS!");
    Ok(http::Response::builder()
        .status(200).body(Some(figure.unwrap().to_string().into()))?)
}
```

### Build and run the application locally

You can try out the application locally by running the following command:

```bash
spin build -u
```

This will build the application and start a local web server. You can run a curl command to test the application:

```bash
curl http://127.0.0.1:3000/api/figlet
```

You should see the following output:

```bash
curl http://127.0.0.1:3000/api/figlet

  _   _          _   _                 _____                                                                            _                                       _      _  __  ____    _ 
 | | | |   ___  | | | |   ___         |  ___|   ___   _ __   _ __ ___    _   _    ___    _ __       ___    _ __        / \     ____  _   _   _ __    ___       / \    | |/ / / ___|  | |
 | |_| |  / _ \ | | | |  / _ \        | |_     / _ \ | '__| | '_ ` _ \  | | | |  / _ \  | '_ \     / _ \  | '_ \      / _ \   |_  / | | | | | '__|  / _ \     / _ \   | ' /  \___ \  | |
 |  _  | |  __/ | | | | | (_) |  _    |  _|   |  __/ | |    | | | | | | | |_| | | (_) | | | | |   | (_) | | | | |    / ___ \   / /  | |_| | | |    |  __/    / ___ \  | . \   ___) | |_|
 |_| |_|  \___| |_| |_|  \___/  ( )   |_|      \___| |_|    |_| |_| |_|  \__, |  \___/  |_| |_|    \___/  |_| |_|   /_/   \_\ /___|  \__,_| |_|     \___|   /_/   \_\ |_|\_\ |____/  (_)
                                |/                                       |___/                                                                                                          

```

### Publish the application to Azure container registry (ACR)

Before we can publish the application to the ACR, we need to create the ACR first. We will do this by extending the
existing Pulumi program.

Add the `containerregistry` package to our `go.mod` file, by running the following command:

```bash
go get -u github.com/pulumi/pulumi-azure-native-sdk/containerregistry
go get -u github.com/pulumi/pulumi-azure-native-sdk/authorization
go get -u github.com/pulumi/pulumi-docker/sdk/v4
```

Now we can add the following code to the `main.go` file:

```go
package main

// ... Omited code

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// ... Omited code

		registry, err := v20230101preview.NewRegistry(ctx, "wasm-aks-registry", &v20230101preview.RegistryArgs{
			ResourceGroupName: resourceGroup.Name,
			Location:          resourceGroup.Location,
			RegistryName:      pulumi.String("wasmaksregistry"),
			AdminUserEnabled:  pulumi.Bool(true),
			Sku: &v20230101preview.SkuArgs{
				Name: pulumi.String("Standard"),
			},
		})
		if err != nil {
			return err
		}

		credentials := pulumi.All(resourceGroup.Name, registry.Name).ApplyT(func(args []interface{}) (*v20230101preview.ListRegistryCredentialsResult, error) {
			return v20230101preview.ListRegistryCredentials(ctx, &v20230101preview.ListRegistryCredentialsArgs{
				ResourceGroupName: args[0].(string),
				RegistryName:      args[1].(string),
			})
		})
		if err != nil {
			return err
		}
		adminUsername := credentials.ApplyT(func(result interface{}) (string, error) {
			credentials := result.(*v20230101preview.ListRegistryCredentialsResult)
			return *credentials.Username, nil
		}).(pulumi.StringOutput)
		adminPassword := credentials.ApplyT(func(result interface{}) (string, error) {
			credentials := result.(*v20230101preview.ListRegistryCredentialsResult)
			return *credentials.Passwords[0].Value, nil
		}).(pulumi.StringOutput)

		definition, err := v20220401.LookupRoleDefinition(ctx, &v20220401.LookupRoleDefinitionArgs{
			RoleDefinitionId: "7f951dda-4ed3-4680-a7ca-43fe172d538d",
		})
		if err != nil {
			return err
		}

		_, err = v20220401.NewRoleAssignment(ctx, "wasm-aks-role-assignment", &v20220401.RoleAssignmentArgs{
			PrincipalId:      wasmCluster.IdentityProfile.MapIndex(pulumi.String("kubeletidentity")).ObjectId().Elem(),
			PrincipalType:    pulumi.String(v20220401.PrincipalTypeServicePrincipal),
			RoleDefinitionId: pulumi.String(definition.Id),
			Scope:            registry.ID(),
		}, pulumi.DependsOn([]pulumi.Resource{registry}))
		if err != nil {
			return err
		}

		image, err := docker.NewImage(ctx, "wasm-spin-demo-image", &docker.ImageArgs{
			ImageName: pulumi.Sprintf("%s.azurecr.io/aks-wasm-spin-demo:latest", registry.Name),
			Build: &docker.DockerBuildArgs{
				Dockerfile:     pulumi.String("aks-spin-demo/Dockerfile"),
				Context:        pulumi.String("aks-spin-demo"),
				BuilderVersion: docker.BuilderVersionBuilderBuildKit,
				Platform:       pulumi.String("linux/amd64"),
			},
			Registry: &docker.RegistryArgs{
				Server:   pulumi.Sprintf("%s.azurecr.io", registry.Name),
				Username: adminUsername,
				Password: adminPassword,
			},
		}, pulumi.DependsOn([]pulumi.Resource{wasmCluster, wasmPool, registry}))

		return nil
	})
}
```

This code will create the ACR resource with admin user enabled. We will use the admin user to push our Spin image to
the ACR. We also create a role assignment to allow the AKS cluster to pull images from the newly created ACR. With this
role assignment in place, we don't need to create a pull secret for the AKS cluster or a specific service account.

The last part of the code will build the image using the `pulumi-docker` provider. I have created a Dockerfile in
the `aks-spin-demo` folder which is a multi-stage build. The first stage will build the Rust application and the second
stage will create a minimal image with the compiled binary and the `spin.toml` file.

> **Attention**: The tag `spin_manifest_version` has to be renamed to `spin_version`, otherwise the shim will not work!

The minimal image is created by using the Chainguard `cgr.dev/chainguard/static` image. The `cgr.dev/chainguard/static`
image is a base image with just enough files to run static binaries!

```Dockerfile
FROM --platform=${BUILDPLATFORM} rust:1.68.1 AS build
WORKDIR /opt/build
COPY . .
RUN rustup target add wasm32-wasi && cargo build --target wasm32-wasi --release

FROM cgr.dev/chainguard/static:latest
COPY --from=build /opt/build/target/wasm32-wasi/release/aks_spin_demo.wasm .
COPY --from=build /opt/build/spin.toml .
```

### Deploy the application to the AKS cluster

Now we can head over to the deployment of the Spin application on our AKS cluster. For this step, we will use
the `pulumi-kubernetes` provider. With this provider, we can use `go` to create the Kubernetes resources.

The resources we will create are:

- A namespace for the application, we name it `wasm-demo`
- A deployment for the application, important is here to set the `command` to `/`
- A service for the application of type `LoadBalancer`

Add the `pulumi-kubernetes` provider to your `go.mod` file:

```bash
go get -u github.com/pulumi/pulumi-kubernetes/sdk/v3
```

```go
package main

// ... Omited code

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// ... Omited code
		k8s, err := kubernetes.NewProvider(ctx, "wasm-aks-provider", &kubernetes.ProviderArgs{
			Kubeconfig:            kubeconfig,
			EnableServerSideApply: pulumi.Bool(true),
		}, pulumi.DependsOn([]pulumi.Resource{wasmCluster, wasmPool, registry}))
		if err != nil {
			return err
		}

		_, err = core.NewNamespace(ctx, "wasm-aks-namespace", &core.NamespaceArgs{
			Metadata: &meta.ObjectMetaArgs{
				Name: pulumi.String("wasm-demo"),
			},
		}, pulumi.Provider(k8s))

		deployment, err := apps.NewDeployment(ctx, "wasm-aks-deployment", &apps.DeploymentArgs{
			Metadata: &meta.ObjectMetaArgs{
				Name:      pulumi.String("wasm-demo"),
				Namespace: pulumi.String("wasm-demo"),
				Annotations: pulumi.StringMap{
					"pulumi.com/skipAwait": pulumi.String("true"),
				},
			},
			Spec: &apps.DeploymentSpecArgs{
				Selector: &meta.LabelSelectorArgs{
					MatchLabels: pulumi.StringMap{
						"app": pulumi.String("wasm-demo"),
					},
				},
				Replicas: pulumi.Int(1),
				Template: &core.PodTemplateSpecArgs{
					Metadata: &meta.ObjectMetaArgs{
						Labels: pulumi.StringMap{
							"app": pulumi.String("wasm-demo"),
						},
					},
					Spec: &core.PodSpecArgs{
						RuntimeClassName: pulumi.String("wasmtime-spin-v1"),
						Containers: core.ContainerArray{
							&core.ContainerArgs{
								Name:  pulumi.String("wasm-demo"),
								Image: image.ImageName,
								Command: pulumi.StringArray{
									pulumi.String("/"),
								},
								Resources: &core.ResourceRequirementsArgs{
									Requests: pulumi.StringMap{
										"cpu":    pulumi.String("10m"),
										"memory": pulumi.String("10Mi"),
									},
									Limits: pulumi.StringMap{
										"cpu":    pulumi.String("500m"),
										"memory": pulumi.String("64Mi"),
									},
								},
							},
						},
					},
				},
			},
		}, pulumi.Provider(k8s), pulumi.DependsOn([]pulumi.Resource{wasmCluster, wasmPool, registry, image}))

		_, err = core.NewService(ctx, "wasm-aks-service", &core.ServiceArgs{
			Metadata: &meta.ObjectMetaArgs{
				Name:      pulumi.String("wasm-demo"),
				Namespace: pulumi.String("wasm-demo"),
				Annotations: pulumi.StringMap{
					"pulumi.com/skipAwait": pulumi.String("true"),
				},
			},
			Spec: &core.ServiceSpecArgs{
				Type: core.ServiceSpecTypeLoadBalancer,
				Ports: core.ServicePortArray{
					&core.ServicePortArgs{
						Name:       pulumi.String("http"),
						Protocol:   pulumi.String("TCP"),
						Port:       pulumi.Int(8080),
						TargetPort: pulumi.Int(80),
					},
				},
				Selector: pulumi.StringMap{
					"app": deployment.Spec.Selector().MatchLabels().MapIndex(pulumi.String("app")),
				},
			},
		}, pulumi.Provider(k8s))
		if err != nil {
			return err
		}
		return nil
	})
}
```

### Test the application

Now we can run the `pulumi up` command to deploy the application to the AKS cluster.

```bash
pulumi up
```

After the deployment is finished, we can get the public IP of the service with the following command:

```bash
kubectl get svc -n wasm-demo wasm-demo -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
20.101.12.51
```

With this IP address, we can now use `curl` to test the application:

```bash
curl http://20.101.12.51:8080/api/figlet
  _   _          _   _                 _____                                                                            _                                       _      _  __  ____    _
 | | | |   ___  | | | |   ___         |  ___|   ___   _ __   _ __ ___    _   _    ___    _ __       ___    _ __        / \     ____  _   _   _ __    ___       / \    | |/ / / ___|  | |
 | |_| |  / _ \ | | | |  / _ \        | |_     / _ \ | '__| | '_ ` _ \  | | | |  / _ \  | '_ \     / _ \  | '_ \      / _ \   |_  / | | | | | '__|  / _ \     / _ \   | ' /  \___ \  | |
 |  _  | |  __/ | | | | | (_) |  _    |  _|   |  __/ | |    | | | | | | | |_| | | (_) | | | | |   | (_) | | | | |    / ___ \   / /  | |_| | | |    |  __/    / ___ \  | . \   ___) | |_|
 |_| |_|  \___| |_| |_|  \___/  ( )   |_|      \___| |_|    |_| |_| |_|  \__, |  \___/  |_| |_|    \___/  |_| |_|   /_/   \_\ /___|  \__,_| |_|     \___|   /_/   \_\ |_|\_\ |____/  (_)
                                |/                                       |___/
```

And it works!

## Housekeeping

To clean up the resources, we can run the `pulumi destroy` command. This will delete all the resources that we just
created.

## Conclusion

The AKS support for WASM is still in preview, but it is already possible to deploy WASM applications to AKS by enabling
the `WasmNodePoolPreview` feature flag.

I think that WASM is a very interesting technology and seeing that major cloud providers like Azure are already starting
to support it is very exciting and the step in the right direction.

I am looking forward to see what the future holds for WASM, and I am sure that we will see more and more WASM
integrations and support from all the major cloud providers.

## Resources

- https://www.pulumi.com/docs/get-started/install/
- https://developer.fermyon.com/spin/index
- https://www.pulumi.com/registry/packages/azure-native/
- https://www.pulumi.com/registry/packages/kubernetes/
- https://www.pulumi.com/registry/packages/docker/
