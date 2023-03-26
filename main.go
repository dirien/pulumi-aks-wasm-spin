package main

import (
	"encoding/base64"

	v20220401 "github.com/pulumi/pulumi-azure-native-sdk/authorization/v20220401"
	"github.com/pulumi/pulumi-azure-native-sdk/containerregistry/v20230101preview"
	containerservice "github.com/pulumi/pulumi-azure-native-sdk/containerservice/v20230101"
	resources "github.com/pulumi/pulumi-azure-native-sdk/resources/v20220901"
	"github.com/pulumi/pulumi-docker/sdk/v4/go/docker"
	"github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes"
	apps "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/apps/v1"
	core "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/core/v1"
	meta "github.com/pulumi/pulumi-kubernetes/sdk/v3/go/kubernetes/meta/v1"
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
		}, pulumi.DependsOn([]pulumi.Resource{wasmCluster, wasmPool, registry}))
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
