// Package ecs discovers ECS services and their CloudWatch log groups.
package ecs

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
)

// ListClusters returns ECS cluster ARNs in the configured region.
func ListClusters(ctx context.Context, cfg aws.Config) ([]string, error) {
	var arns []string
	p := awsecs.NewListClustersPaginator(awsecs.NewFromConfig(cfg), &awsecs.ListClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.ClusterArns...)
	}
	sort.Strings(arns)
	return arns, nil
}

// ListServices returns service ARNs in cluster.
func ListServices(ctx context.Context, cfg aws.Config, cluster string) ([]string, error) {
	var arns []string
	p := awsecs.NewListServicesPaginator(awsecs.NewFromConfig(cfg), &awsecs.ListServicesInput{Cluster: aws.String(cluster)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.ServiceArns...)
	}
	sort.Strings(arns)
	return arns, nil
}

// LogGroups returns the distinct awslogs groups used by service's task definition.
func LogGroups(ctx context.Context, cfg aws.Config, cluster, service string) ([]string, error) {
	client := awsecs.NewFromConfig(cfg)
	svc, err := client.DescribeServices(ctx, &awsecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []string{service},
	})
	if err != nil {
		return nil, fmt.Errorf("describe ECS service: %w", err)
	}
	if len(svc.Services) == 0 || svc.Services[0].TaskDefinition == nil {
		return nil, fmt.Errorf("ECS service %s not found in %s", service, cluster)
	}
	td, err := client.DescribeTaskDefinition(ctx, &awsecs.DescribeTaskDefinitionInput{TaskDefinition: svc.Services[0].TaskDefinition})
	if err != nil {
		return nil, fmt.Errorf("describe task definition: %w", err)
	}
	seen := map[string]bool{}
	var groups []string
	for _, c := range td.TaskDefinition.ContainerDefinitions {
		if c.LogConfiguration == nil {
			continue
		}
		if g := c.LogConfiguration.Options["awslogs-group"]; g != "" && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("task definition of %s has no awslogs log group", service)
	}
	sort.Strings(groups)
	return groups, nil
}
