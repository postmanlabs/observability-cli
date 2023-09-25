### Amazon Elastic Container Service (ECS)

### Introduction

- The Postman Live Collection Agent(LCA) attaches as a side car to the specified service
- Postman collection is populated with endpoints observed from the traffic arrving on your service

- Both EC2 and Fargate capactiy providers are supported

### Pre-requistites
- AWS credentials stored at `~/.aws/credentials` 
- Your aws credentails **must have** these AWS permissions [Setup ECS Permissions](#setup-aws-ecs-permissions)
- ECS service must have public internet access. [Docs: Ensure Internet Access](#ensure-internet-access)

### Usage

```
POSTMAN_API_KEY=<postman-api-key> postman-lc-agent ecs add \
--collection <postman-collectionID> \
--region <aws-region> \
--cluster <full ARN of ECS cluster> \
--service <full ARN of ECS service>
```

**NOTE**: Updating your service with newly modified task definition might take time, please check AWS console for the progress.

#### Additional Configuration

- See help menu for further configuration
```
postman-lc-agent ecs --help
```



### Uninstall
- Update your ECS service to old revision of task definition.

### Setup AWS ECS Permissions

- Attach the following policy to your aws profile

```
{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Action": [
				"ecs:UpdateService",
				"ecs:RegisterTaskDefinition",
				"ecs:DescribeServices",
				"ecs:TagResource",
				"ecs:DescribeTaskDefinition",
				"ecs:DescribeClusters"
			],
			"Resource": "*"
		}
	]
}
```
- **Instead** of the above policy [AmazonECS_FullAccess](https://docs.aws.amazon.com/AmazonECS/latest/userguide/security-iam-awsmanpol.html#security-iam-awsmanpol-AmazonECS_FullAccess) can also be used to ensure easy authoraization.

### Ensure internet access
#### Fargate tasks
- When using a public subnet, you can assign a public IP address to the task ENI.
- When using a private subnet, the subnet can have a NAT gateway attached.
- AWS Docs: See [Task networking for tasks hosted on Fargate](https://docs.aws.amazon.com/AmazonECS/latest/userguide/fargate-task-networking.html).

#### EC2 tasks
- Tasks must be launched in private subnets with NAT gateway. 
- For more information, see [Task networking for tasks that are hosted on Amazon EC2 instances](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-networking.html)

