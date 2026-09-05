package main

import (
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	awsprovider "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/acm"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudfront"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/route53"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sns"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi-tls/sdk/v5/go/tls"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		stack := ctx.Stack()
		cfg := config.New(ctx, "sema")
		googleClientID := cfg.Require("googleClientId")
		domainName := strings.TrimSpace(cfg.Get("domain"))
		route53ZoneID := strings.TrimSpace(cfg.Get("route53ZoneId"))
		if route53ZoneID == "" {
			route53ZoneID = strings.TrimSpace(cfg.Get("zoneId"))
		}
		modelVersion := cfg.Get("modelVersion")
		if modelVersion == "" {
			modelVersion = "amazon.titan-embed-text-v2:0"
		}
		summarizeModel := cfg.Get("summarizeModel")
		if summarizeModel == "" {
			summarizeModel = "amazon.nova-micro-v1:0"
		}
		scoringVersion := cfg.Get("scoringVersion")
		if scoringVersion == "" {
			scoringVersion = "2"
		}
		storySimilarity := cfg.GetInt("storySimilarity")
		if storySimilarity == 0 {
			storySimilarity = 80
		}
		storyWindowHours := cfg.GetInt("storyWindowHours")
		if storyWindowHours == 0 {
			storyWindowHours = 72
		}
		youtubeDiscoveryEnabled := !strings.EqualFold(strings.TrimSpace(cfg.Get("youtubeDiscoveryEnabled")), "false")
		vectorIndexName := strings.TrimSpace(cfg.Get("vectorIndex"))
		if vectorIndexName == "" {
			vectorIndexName = "items"
		}
		vectorDimensions := cfg.GetInt("vectorDimensions")
		if vectorDimensions == 0 {
			vectorDimensions = 1024
		}
		alarmTopicName := cfg.Get("alarmTopicName")
		if alarmTopicName == "" {
			alarmTopicName = "NotifyMe"
		}
		alarmTopic, err := sns.LookupTopic(ctx, &sns.LookupTopicArgs{Name: alarmTopicName})
		if err != nil {
			return fmt.Errorf("look up alarm topic %q: %w", alarmTopicName, err)
		}
		alarmActions := pulumi.Array{pulumi.String(alarmTopic.Arn)}
		if err := buildDeployAssets(googleClientID); err != nil {
			return err
		}
		signingKey, err := tls.NewPrivateKey(ctx, "content-signing-key", &tls.PrivateKeyArgs{Algorithm: pulumi.String("RSA"), RsaBits: pulumi.Int(2048)})
		if err != nil {
			return err
		}
		publicKey, err := cloudfront.NewPublicKey(ctx, "content-public-key", &cloudfront.PublicKeyArgs{Name: pulumi.Sprintf("sema-%s", stack), EncodedKey: signingKey.PublicKeyPem})
		if err != nil {
			return err
		}
		keyGroup, err := cloudfront.NewKeyGroup(ctx, "content-key-group", &cloudfront.KeyGroupArgs{Name: pulumi.Sprintf("sema-%s", stack), Items: pulumi.StringArray{publicKey.ID()}})
		if err != nil {
			return err
		}

		table, err := dynamodb.NewTable(ctx, "table", &dynamodb.TableArgs{
			Name:        pulumi.Sprintf("sema-%s", stack),
			BillingMode: pulumi.String("PAY_PER_REQUEST"),
			HashKey:     pulumi.String("PK"),
			RangeKey:    pulumi.String("SK"),
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{Name: pulumi.String("PK"), Type: pulumi.String("S")},
				&dynamodb.TableAttributeArgs{Name: pulumi.String("SK"), Type: pulumi.String("S")},
				&dynamodb.TableAttributeArgs{Name: pulumi.String("score"), Type: pulumi.String("N")},
				&dynamodb.TableAttributeArgs{Name: pulumi.String("gsi1pk"), Type: pulumi.String("S")},
				&dynamodb.TableAttributeArgs{Name: pulumi.String("next_fetch_at"), Type: pulumi.String("S")},
			},
			GlobalSecondaryIndexes: dynamodb.TableGlobalSecondaryIndexArray{
				&dynamodb.TableGlobalSecondaryIndexArgs{Name: pulumi.String("by-score"), KeySchemas: dynamodb.TableGlobalSecondaryIndexKeySchemaArray{
					&dynamodb.TableGlobalSecondaryIndexKeySchemaArgs{AttributeName: pulumi.String("PK"), KeyType: pulumi.String("HASH")},
					&dynamodb.TableGlobalSecondaryIndexKeySchemaArgs{AttributeName: pulumi.String("score"), KeyType: pulumi.String("RANGE")},
				}, ProjectionType: pulumi.String("ALL")},
				&dynamodb.TableGlobalSecondaryIndexArgs{Name: pulumi.String("by-next-fetch"), KeySchemas: dynamodb.TableGlobalSecondaryIndexKeySchemaArray{
					&dynamodb.TableGlobalSecondaryIndexKeySchemaArgs{AttributeName: pulumi.String("gsi1pk"), KeyType: pulumi.String("HASH")},
					&dynamodb.TableGlobalSecondaryIndexKeySchemaArgs{AttributeName: pulumi.String("next_fetch_at"), KeyType: pulumi.String("RANGE")},
				}, ProjectionType: pulumi.String("KEYS_ONLY")},
			},
			Ttl:  &dynamodb.TableTtlArgs{AttributeName: pulumi.String("ttl"), Enabled: pulumi.Bool(true)},
			Tags: pulumi.StringMap{"app": pulumi.String("sema"), "stack": pulumi.String(stack)},
		})
		if err != nil {
			return err
		}

		appBucket, err := s3.NewBucket(ctx, "app-bucket", &s3.BucketArgs{BucketPrefix: pulumi.Sprintf("sema-app-%s-", stack), ForceDestroy: pulumi.Bool(false)})
		if err != nil {
			return err
		}
		contentBucket, err := s3.NewBucket(ctx, "content-bucket", &s3.BucketArgs{BucketPrefix: pulumi.Sprintf("sema-content-%s-", stack), ForceDestroy: pulumi.Bool(false)})
		if err != nil {
			return err
		}
		vectorBucket, err := s3.NewVectorsVectorBucket(ctx, "vector-bucket", &s3.VectorsVectorBucketArgs{
			VectorBucketName: pulumi.Sprintf("sema-vectors-%s", stack), ForceDestroy: pulumi.Bool(false),
			Tags: pulumi.StringMap{"app": pulumi.String("sema"), "stack": pulumi.String(stack)},
		})
		if err != nil {
			return err
		}
		vectorIndex, err := s3.NewVectorsIndex(ctx, "vector-index", &s3.VectorsIndexArgs{
			VectorBucketName: vectorBucket.VectorBucketName, IndexName: pulumi.String(vectorIndexName),
			DataType: pulumi.String("float32"), Dimension: pulumi.Int(vectorDimensions), DistanceMetric: pulumi.String("cosine"),
			MetadataConfiguration: &s3.VectorsIndexMetadataConfigurationArgs{NonFilterableMetadataKeys: pulumi.StringArray{pulumi.String("title")}},
			Tags:                  pulumi.StringMap{"app": pulumi.String("sema"), "stack": pulumi.String(stack)},
		})
		if err != nil {
			return err
		}
		for name, bucket := range map[string]*s3.Bucket{"app": appBucket, "content": contentBucket} {
			if _, err := s3.NewBucketPublicAccessBlock(ctx, name+"-public-access", &s3.BucketPublicAccessBlockArgs{
				Bucket: bucket.ID(), BlockPublicAcls: pulumi.Bool(true), BlockPublicPolicy: pulumi.Bool(true), IgnorePublicAcls: pulumi.Bool(true), RestrictPublicBuckets: pulumi.Bool(true),
			}); err != nil {
				return err
			}
		}
		if _, err := s3.NewBucketLifecycleConfigurationV2(ctx, "content-lifecycle", &s3.BucketLifecycleConfigurationV2Args{
			Bucket: contentBucket.ID(), Rules: s3.BucketLifecycleConfigurationV2RuleArray{
				// Keep these rules prefix-scoped: archive/ contains user-kept copies
				// that must never inherit the rolling seven-day expiry.
				&s3.BucketLifecycleConfigurationV2RuleArgs{Id: pulumi.String("expire-bodies"), Status: pulumi.String("Enabled"), Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{Prefix: pulumi.String("bodies/")}, Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{Days: pulumi.Int(7)}},
				&s3.BucketLifecycleConfigurationV2RuleArgs{Id: pulumi.String("expire-media"), Status: pulumi.String("Enabled"), Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{Prefix: pulumi.String("media/")}, Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{Days: pulumi.Int(7)}},
			},
		}); err != nil {
			return err
		}

		feedsDLQ, feedsQueue, err := queues(ctx, "feeds", 90)
		if err != nil {
			return err
		}
		itemsDLQ, itemsQueue, err := queues(ctx, "items", 180)
		if err != nil {
			return err
		}

		common := pulumi.StringMap{
			"TABLE_NAME": table.Name, "CONTENT_BUCKET": contentBucket.Bucket,
			"MODEL_VERSION": pulumi.String(modelVersion), "SCORING_VERSION": pulumi.String(scoringVersion), "SUMMARIZE_MODEL": pulumi.String(summarizeModel),
			"VECTOR_BUCKET": vectorBucket.VectorBucketName, "VECTOR_INDEX": vectorIndex.IndexName,
		}
		storyEnvironment := pulumi.StringMap{
			"STORY_SIMILARITY": pulumi.Sprintf("%d", storySimilarity), "STORY_WINDOW_HOURS": pulumi.Sprintf("%d", storyWindowHours),
		}
		schedulerRole, err := lambdaRole(ctx, "scheduler", table.Arn, contentBucket.Arn, feedsQueue.Arn, itemsQueue.Arn, vectorIndex.IndexArn, "")
		if err != nil {
			return err
		}
		feedRole, err := lambdaRole(ctx, "feed-worker", table.Arn, contentBucket.Arn, feedsQueue.Arn, itemsQueue.Arn, vectorIndex.IndexArn, "")
		if err != nil {
			return err
		}
		itemRole, err := lambdaRole(ctx, "item-worker", table.Arn, contentBucket.Arn, feedsQueue.Arn, itemsQueue.Arn, vectorIndex.IndexArn, modelVersion, summarizeModel)
		if err != nil {
			return err
		}
		apiRole, err := lambdaRole(ctx, "api", table.Arn, contentBucket.Arn, feedsQueue.Arn, itemsQueue.Arn, vectorIndex.IndexArn, modelVersion)
		if err != nil {
			return err
		}
		rescoreRole, err := lambdaRole(ctx, "rescore", table.Arn, contentBucket.Arn, feedsQueue.Arn, itemsQueue.Arn, vectorIndex.IndexArn, "")
		if err != nil {
			return err
		}
		cleanupRole, err := lambdaRole(ctx, "vector-cleanup", table.Arn, contentBucket.Arn, feedsQueue.Arn, itemsQueue.Arn, vectorIndex.IndexArn, "")
		if err != nil {
			return err
		}

		scheduler, err := function(ctx, "scheduler", schedulerRole, 128, 30, 0, merge(common, pulumi.StringMap{"FEEDS_QUEUE_URL": feedsQueue.Url}))
		if err != nil {
			return err
		}
		feedWorker, err := function(ctx, "feed-worker", feedRole, 256, 60, 10, merge(common, pulumi.StringMap{"ITEMS_QUEUE_URL": itemsQueue.Url}))
		if err != nil {
			return err
		}
		itemWorker, err := function(ctx, "item-worker", itemRole, 1024, 120, 10, merge(common, storyEnvironment))
		if err != nil {
			return err
		}
		rescoreLambda, err := function(ctx, "rescore", rescoreRole, 256, 300, 0, merge(common, storyEnvironment))
		if err != nil {
			return err
		}
		cleanupLambda, err := function(ctx, "vector-cleanup", cleanupRole, 128, 300, 0, common)
		if err != nil {
			return err
		}
		apiLambda, err := function(ctx, "api", apiRole, 1024, 29, 0, merge(common, storyEnvironment, pulumi.StringMap{
			"FEEDS_QUEUE_URL": feedsQueue.Url, "ITEMS_QUEUE_URL": itemsQueue.Url, "CF_PRIVATE_KEY": signingKey.PrivateKeyPem, "CF_KEY_PAIR_ID": publicKey.ID(), "RESCORE_FUNCTION_NAME": rescoreLambda.Name,
			"GOOGLE_CLIENT_ID": pulumi.String(googleClientID), "YOUTUBE_DISCOVERY_ENABLED": pulumi.Sprintf("%t", youtubeDiscoveryEnabled),
		}))
		if err != nil {
			return err
		}
		if _, err := iam.NewRolePolicy(ctx, "api-rescore-invoke", &iam.RolePolicyArgs{
			Role:   apiRole.ID(),
			Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"lambda:InvokeFunction","Resource":"%s"}]}`, rescoreLambda.Arn),
		}); err != nil {
			return err
		}

		if _, err := awslambda.NewEventSourceMapping(ctx, "feed-events", queueEventSourceMappingArgs(feedsQueue.Arn, feedWorker.Arn, 10)); err != nil {
			return err
		}
		if _, err := awslambda.NewEventSourceMapping(ctx, "item-events", queueEventSourceMappingArgs(itemsQueue.Arn, itemWorker.Arn, 5)); err != nil {
			return err
		}

		// Keep the old Pulumi name to avoid replacing the rule.
		rule, err := cloudwatch.NewEventRule(ctx, "hourly", &cloudwatch.EventRuleArgs{ScheduleExpression: pulumi.String("rate(1 minute)")})
		if err != nil {
			return err
		}
		if _, err := cloudwatch.NewEventTarget(ctx, "scheduler-target", &cloudwatch.EventTargetArgs{Rule: rule.Name, Arn: scheduler.Arn}); err != nil {
			return err
		}
		if _, err := awslambda.NewPermission(ctx, "scheduler-permission", &awslambda.PermissionArgs{Action: pulumi.String("lambda:InvokeFunction"), Function: scheduler.Name, Principal: pulumi.String("events.amazonaws.com"), SourceArn: rule.Arn}); err != nil {
			return err
		}
		rescoreRule, err := cloudwatch.NewEventRule(ctx, "nightly-rescore", &cloudwatch.EventRuleArgs{ScheduleExpression: pulumi.String("cron(0 9 * * ? *)")})
		if err != nil {
			return err
		}
		if _, err := cloudwatch.NewEventTarget(ctx, "rescore-target", &cloudwatch.EventTargetArgs{Rule: rescoreRule.Name, Arn: rescoreLambda.Arn}); err != nil {
			return err
		}
		if _, err := awslambda.NewPermission(ctx, "rescore-permission", &awslambda.PermissionArgs{Action: pulumi.String("lambda:InvokeFunction"), Function: rescoreLambda.Name, Principal: pulumi.String("events.amazonaws.com"), SourceArn: rescoreRule.Arn}); err != nil {
			return err
		}
		cleanupRule, err := cloudwatch.NewEventRule(ctx, "weekly-vector-cleanup", &cloudwatch.EventRuleArgs{ScheduleExpression: pulumi.String("cron(0 10 ? * SUN *)")})
		if err != nil {
			return err
		}
		if _, err := cloudwatch.NewEventTarget(ctx, "vector-cleanup-target", &cloudwatch.EventTargetArgs{Rule: cleanupRule.Name, Arn: cleanupLambda.Arn}); err != nil {
			return err
		}
		if _, err := awslambda.NewPermission(ctx, "vector-cleanup-permission", &awslambda.PermissionArgs{Action: pulumi.String("lambda:InvokeFunction"), Function: cleanupLambda.Name, Principal: pulumi.String("events.amazonaws.com"), SourceArn: cleanupRule.Arn}); err != nil {
			return err
		}

		httpAPI, err := apigatewayv2.NewApi(ctx, "api-gateway", &apigatewayv2.ApiArgs{ProtocolType: pulumi.String("HTTP")})
		if err != nil {
			return err
		}
		integration, err := apigatewayv2.NewIntegration(ctx, "api-integration", &apigatewayv2.IntegrationArgs{
			ApiId: httpAPI.ID(), IntegrationType: pulumi.String("AWS_PROXY"), IntegrationUri: apiLambda.Arn, PayloadFormatVersion: pulumi.String("2.0"), TimeoutMilliseconds: pulumi.Int(29000),
		})
		if err != nil {
			return err
		}
		for name, routeKey := range map[string]string{"api-root": "ANY /api", "api-proxy": "ANY /api/{proxy+}"} {
			if _, err := apigatewayv2.NewRoute(ctx, name, &apigatewayv2.RouteArgs{
				ApiId: httpAPI.ID(), RouteKey: pulumi.String(routeKey), Target: pulumi.Sprintf("integrations/%s", integration.ID()), AuthorizationType: pulumi.String("NONE"),
			}); err != nil {
				return err
			}
		}
		if _, err := apigatewayv2.NewStage(ctx, "default-stage", &apigatewayv2.StageArgs{ApiId: httpAPI.ID(), Name: pulumi.String("$default"), AutoDeploy: pulumi.Bool(true)}); err != nil {
			return err
		}
		if _, err := awslambda.NewPermission(ctx, "api-permission", &awslambda.PermissionArgs{
			Action: pulumi.String("lambda:InvokeFunction"), Function: apiLambda.Name, Principal: pulumi.String("apigateway.amazonaws.com"), SourceArn: pulumi.Sprintf("%s/*/*", httpAPI.ExecutionArn),
		}); err != nil {
			return err
		}

		oac, err := cloudfront.NewOriginAccessControl(ctx, "oac", &cloudfront.OriginAccessControlArgs{
			Name: pulumi.Sprintf("sema-%s", stack), OriginAccessControlOriginType: pulumi.String("s3"), SigningBehavior: pulumi.String("always"), SigningProtocol: pulumi.String("sigv4"),
		})
		if err != nil {
			return err
		}
		spaRewrite, err := cloudfront.NewFunction(ctx, "spa-rewrite", &cloudfront.FunctionArgs{
			Name: pulumi.Sprintf("sema-%s-spa", stack), Runtime: pulumi.String("cloudfront-js-2.0"), Publish: pulumi.Bool(true),
			Code: pulumi.String(`function handler(event) { var request = event.request; if (request.uri === "/" || request.uri.lastIndexOf(".") < request.uri.lastIndexOf("/")) request.uri = "/index.html"; return request; }`),
		})
		if err != nil {
			return err
		}
		apiDomain := httpAPI.ApiEndpoint.ApplyT(func(endpoint string) string { return strings.TrimPrefix(endpoint, "https://") }).(pulumi.StringOutput)
		defaultBehavior := staticBehavior("app")
		defaultBehavior.FunctionAssociations = cloudfront.DistributionDefaultCacheBehaviorFunctionAssociationArray{
			&cloudfront.DistributionDefaultCacheBehaviorFunctionAssociationArgs{EventType: pulumi.String("viewer-request"), FunctionArn: spaRewrite.Arn},
		}
		aliases := pulumi.StringArray{}
		viewerCertificate := &cloudfront.DistributionViewerCertificateArgs{CloudfrontDefaultCertificate: pulumi.Bool(true), MinimumProtocolVersion: pulumi.String("TLSv1.2_2021")}
		var certificateValidationName, certificateValidationValue pulumi.StringOutput
		if domainName != "" {
			usEast1, providerErr := awsprovider.NewProvider(ctx, "acm-us-east-1", &awsprovider.ProviderArgs{Region: pulumi.String("us-east-1")})
			if providerErr != nil {
				return providerErr
			}
			certificate, certificateErr := acm.NewCertificate(ctx, "domain-certificate", &acm.CertificateArgs{
				DomainName: pulumi.String(domainName), ValidationMethod: pulumi.String("DNS"),
			}, pulumi.Provider(usEast1))
			if certificateErr != nil {
				return certificateErr
			}
			validationOption := certificate.DomainValidationOptions.Index(pulumi.Int(0))
			certificateValidationName = requiredString(validationOption.ResourceRecordName())
			certificateValidationValue = requiredString(validationOption.ResourceRecordValue())
			var validationFQDNs pulumi.StringArray
			if route53ZoneID != "" {
				validationRecord, recordErr := route53.NewRecord(ctx, "domain-certificate-validation", &route53.RecordArgs{
					ZoneId: pulumi.String(route53ZoneID), Name: certificateValidationName, Type: pulumi.String("CNAME"),
					Ttl: pulumi.Int(300), Records: pulumi.StringArray{certificateValidationValue}, AllowOverwrite: pulumi.Bool(true),
				})
				if recordErr != nil {
					return recordErr
				}
				validationFQDNs = pulumi.StringArray{validationRecord.Fqdn}
			}
			validated, validationErr := acm.NewCertificateValidation(ctx, "domain-certificate-validation-waiter", &acm.CertificateValidationArgs{
				CertificateArn: certificate.Arn, ValidationRecordFqdns: validationFQDNs,
			}, pulumi.Provider(usEast1))
			if validationErr != nil {
				return validationErr
			}
			aliases = pulumi.StringArray{pulumi.String(domainName)}
			viewerCertificate = &cloudfront.DistributionViewerCertificateArgs{
				AcmCertificateArn: validated.CertificateArn, SslSupportMethod: pulumi.String("sni-only"), MinimumProtocolVersion: pulumi.String("TLSv1.2_2021"),
			}
		}
		distribution, err := cloudfront.NewDistribution(ctx, "cdn", &cloudfront.DistributionArgs{
			Enabled: pulumi.Bool(true), IsIpv6Enabled: pulumi.Bool(true), DefaultRootObject: pulumi.String("index.html"), PriceClass: pulumi.String("PriceClass_100"), Aliases: aliases,
			Origins: cloudfront.DistributionOriginArray{
				&cloudfront.DistributionOriginArgs{DomainName: appBucket.BucketRegionalDomainName, OriginId: pulumi.String("app"), OriginAccessControlId: oac.ID(), S3OriginConfig: &cloudfront.DistributionOriginS3OriginConfigArgs{OriginAccessIdentity: pulumi.String("")}},
				&cloudfront.DistributionOriginArgs{DomainName: contentBucket.BucketRegionalDomainName, OriginId: pulumi.String("content"), OriginAccessControlId: oac.ID(), S3OriginConfig: &cloudfront.DistributionOriginS3OriginConfigArgs{OriginAccessIdentity: pulumi.String("")}},
				&cloudfront.DistributionOriginArgs{DomainName: apiDomain, OriginId: pulumi.String("api"), CustomOriginConfig: &cloudfront.DistributionOriginCustomOriginConfigArgs{HttpPort: pulumi.Int(80), HttpsPort: pulumi.Int(443), OriginProtocolPolicy: pulumi.String("https-only"), OriginSslProtocols: pulumi.StringArray{pulumi.String("TLSv1.2")}}},
			},
			DefaultCacheBehavior: defaultBehavior,
			OrderedCacheBehaviors: cloudfront.DistributionOrderedCacheBehaviorArray{
				contentBehavior("/bodies/*", "content", keyGroup.ID()), contentBehavior("/media/*", "content", keyGroup.ID()), contentBehavior("/archive/*", "content", keyGroup.ID()), contentBehavior("/favicons/*", "content", nil), apiBehavior("/api/*", "api"),
			},
			Restrictions:      &cloudfront.DistributionRestrictionsArgs{GeoRestriction: &cloudfront.DistributionRestrictionsGeoRestrictionArgs{RestrictionType: pulumi.String("none")}},
			ViewerCertificate: viewerCertificate,
		})
		if err != nil {
			return err
		}
		if domainName != "" && route53ZoneID != "" {
			for _, recordType := range []string{"A", "AAAA"} {
				if _, err := route53.NewRecord(ctx, "domain-"+strings.ToLower(recordType), &route53.RecordArgs{
					ZoneId: pulumi.String(route53ZoneID), Name: pulumi.String(domainName), Type: pulumi.String(recordType),
					Aliases: route53.RecordAliasArray{&route53.RecordAliasArgs{Name: distribution.DomainName, ZoneId: distribution.HostedZoneId, EvaluateTargetHealth: pulumi.Bool(false)}},
				}); err != nil {
					return err
				}
			}
		}
		for name, bucket := range map[string]*s3.Bucket{"app": appBucket, "content": contentBucket} {
			policy := pulumi.All(bucket.Arn, distribution.Arn).ApplyT(func(values []any) (string, error) {
				doc := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{
					"Effect": "Allow", "Principal": map[string]string{"Service": "cloudfront.amazonaws.com"}, "Action": "s3:GetObject", "Resource": values[0].(string) + "/*",
					"Condition": map[string]any{"StringEquals": map[string]string{"AWS:SourceArn": values[1].(string)}},
				}}}
				encoded, err := json.Marshal(doc)
				return string(encoded), err
			}).(pulumi.StringOutput)
			if _, err := s3.NewBucketPolicy(ctx, name+"-policy", &s3.BucketPolicyArgs{Bucket: bucket.ID(), Policy: policy}); err != nil {
				return err
			}
		}
		if err := uploadWeb(ctx, appBucket); err != nil {
			return err
		}

		for name, queue := range map[string]*sqs.Queue{"feeds": feedsDLQ, "items": itemsDLQ} {
			if _, err := cloudwatch.NewMetricAlarm(ctx, name+"-dlq-alarm", &cloudwatch.MetricAlarmArgs{
				Namespace: pulumi.String("AWS/SQS"), MetricName: pulumi.String("ApproximateNumberOfMessagesVisible"), Statistic: pulumi.String("Maximum"), Period: pulumi.Int(60), EvaluationPeriods: pulumi.Int(1), ComparisonOperator: pulumi.String("GreaterThanThreshold"), Threshold: pulumi.Float64(0),
				Dimensions: pulumi.StringMap{"QueueName": queue.Name}, AlarmDescription: pulumi.String(name + " dead-letter queue contains messages"),
			}); err != nil {
				return err
			}
		}
		if _, err := cloudwatch.NewMetricAlarm(ctx, "scheduler-missed", &cloudwatch.MetricAlarmArgs{
			Namespace: pulumi.String("AWS/Lambda"), MetricName: pulumi.String("Invocations"), Statistic: pulumi.String("Sum"), Period: pulumi.Int(7200), EvaluationPeriods: pulumi.Int(1), ComparisonOperator: pulumi.String("LessThanThreshold"), Threshold: pulumi.Float64(1), TreatMissingData: pulumi.String("breaching"), Dimensions: pulumi.StringMap{"FunctionName": scheduler.Name},
		}); err != nil {
			return err
		}
		if _, err := cloudwatch.NewMetricAlarm(ctx, "scheduler-silent", schedulerSilentAlarmArgs(alarmActions)); err != nil {
			return err
		}
		if _, err := cloudwatch.NewMetricAlarm(ctx, "item-worker-errors", &cloudwatch.MetricAlarmArgs{
			EvaluationPeriods: pulumi.Int(1), ComparisonOperator: pulumi.String("GreaterThanThreshold"), Threshold: pulumi.Float64(0.05), TreatMissingData: pulumi.String("notBreaching"),
			MetricQueries: cloudwatch.MetricAlarmMetricQueryArray{
				&cloudwatch.MetricAlarmMetricQueryArgs{Id: pulumi.String("rate"), Expression: pulumi.String("IF(invocations>0,errors/invocations,0)"), Label: pulumi.String("item worker error rate"), ReturnData: pulumi.Bool(true)},
				&cloudwatch.MetricAlarmMetricQueryArgs{Id: pulumi.String("errors"), ReturnData: pulumi.Bool(false), Metric: &cloudwatch.MetricAlarmMetricQueryMetricArgs{Namespace: pulumi.String("AWS/Lambda"), MetricName: pulumi.String("Errors"), Period: pulumi.Int(300), Stat: pulumi.String("Sum"), Dimensions: pulumi.StringMap{"FunctionName": itemWorker.Name}}},
				&cloudwatch.MetricAlarmMetricQueryArgs{Id: pulumi.String("invocations"), ReturnData: pulumi.Bool(false), Metric: &cloudwatch.MetricAlarmMetricQueryMetricArgs{Namespace: pulumi.String("AWS/Lambda"), MetricName: pulumi.String("Invocations"), Period: pulumi.Int(300), Stat: pulumi.String("Sum"), Dimensions: pulumi.StringMap{"FunctionName": itemWorker.Name}}},
			},
		}); err != nil {
			return err
		}
		if _, err := cloudwatch.NewMetricAlarm(ctx, "generated-summaries-daily", &cloudwatch.MetricAlarmArgs{
			Namespace: pulumi.String("Sema"), MetricName: pulumi.String("SummariesGenerated"), Statistic: pulumi.String("Sum"), Period: pulumi.Int(86400), EvaluationPeriods: pulumi.Int(1),
			ComparisonOperator: pulumi.String("GreaterThanThreshold"), Threshold: pulumi.Float64(2000), TreatMissingData: pulumi.String("notBreaching"), AlarmActions: alarmActions,
			AlarmDescription: pulumi.String("summary generation exceeded the 2,000 item daily cost guard"),
		}); err != nil {
			return err
		}

		cloudfrontURL := pulumi.Sprintf("https://%s", distribution.DomainName)
		ctx.Export("cloudfrontUrl", cloudfrontURL)
		if domainName != "" {
			ctx.Export("url", pulumi.String("https://"+domainName))
			ctx.Export("dnsCnameTarget", distribution.DomainName)
			ctx.Export("certificateValidationName", certificateValidationName)
			ctx.Export("certificateValidationValue", certificateValidationValue)
		} else {
			ctx.Export("url", cloudfrontURL)
		}
		ctx.Export("distributionId", distribution.ID())
		ctx.Export("appBucket", appBucket.Bucket)
		ctx.Export("contentBucket", contentBucket.Bucket)
		ctx.Export("vectorBucket", vectorBucket.VectorBucketName)
		ctx.Export("vectorIndex", vectorIndex.IndexName)
		ctx.Export("itemsQueueUrl", itemsQueue.Url)
		ctx.Export("feedsQueueArn", feedsQueue.Arn)
		ctx.Export("feedsDlqArn", feedsDLQ.Arn)
		ctx.Export("itemsQueueArn", itemsQueue.Arn)
		ctx.Export("itemsDlqArn", itemsDLQ.Arn)
		ctx.Export("rescoreFunction", rescoreLambda.Name)
		ctx.Export("modelVersion", pulumi.String(modelVersion))
		ctx.Export("apiEndpoint", httpAPI.ApiEndpoint)
		return nil
	})
}

func requiredString(value pulumi.StringPtrOutput) pulumi.StringOutput {
	return value.ApplyT(func(pointer *string) string {
		if pointer == nil {
			return ""
		}
		return *pointer
	}).(pulumi.StringOutput)
}

func schedulerSilentAlarmArgs(alarmActions pulumi.ArrayInput) *cloudwatch.MetricAlarmArgs {
	return &cloudwatch.MetricAlarmArgs{
		Namespace:          pulumi.String("Sema"),
		MetricName:         pulumi.String("FeedsEnqueued"),
		Statistic:          pulumi.String("Sum"),
		Period:             pulumi.Int(3600),
		EvaluationPeriods:  pulumi.Int(4),
		DatapointsToAlarm:  pulumi.Int(4),
		ComparisonOperator: pulumi.String("LessThanThreshold"),
		Threshold:          pulumi.Float64(1),
		TreatMissingData:   pulumi.String("breaching"),
		AlarmActions:       alarmActions,
		AlarmDescription:   pulumi.String("scheduler enqueued no feeds for four consecutive hourly periods"),
	}
}

func queueEventSourceMappingArgs(eventSourceArn, functionName pulumi.StringInput, batchSize int) *awslambda.EventSourceMappingArgs {
	return &awslambda.EventSourceMappingArgs{
		EventSourceArn:        eventSourceArn,
		FunctionName:          functionName,
		BatchSize:             pulumi.Int(batchSize),
		FunctionResponseTypes: pulumi.StringArray{pulumi.String("ReportBatchItemFailures")},
		ScalingConfig:         &awslambda.EventSourceMappingScalingConfigArgs{MaximumConcurrency: pulumi.Int(10)},
	}
}

func queues(ctx *pulumi.Context, name string, visibility int) (*sqs.Queue, *sqs.Queue, error) {
	dlq, err := sqs.NewQueue(ctx, name+"-dlq", &sqs.QueueArgs{MessageRetentionSeconds: pulumi.Int(1209600)})
	if err != nil {
		return nil, nil, err
	}
	redrive := dlq.Arn.ApplyT(func(arn string) (string, error) {
		value, err := json.Marshal(map[string]any{"deadLetterTargetArn": arn, "maxReceiveCount": 3})
		return string(value), err
	}).(pulumi.StringOutput)
	queue, err := sqs.NewQueue(ctx, name+"-queue", &sqs.QueueArgs{VisibilityTimeoutSeconds: pulumi.Int(visibility), RedrivePolicy: redrive})
	return dlq, queue, err
}

func lambdaRole(ctx *pulumi.Context, name string, tableArn, bucketArn, feedsArn, itemsArn, vectorIndexArn pulumi.StringOutput, bedrockModels ...string) (*iam.Role, error) {
	role, err := iam.NewRole(ctx, name+"-role", &iam.RoleArgs{AssumeRolePolicy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`)})
	if err != nil {
		return nil, err
	}
	policy := pulumi.All(tableArn, bucketArn, feedsArn, itemsArn, vectorIndexArn).ApplyT(func(values []any) (string, error) {
		tableResources := []string{values[0].(string), values[0].(string) + "/index/*"}
		queueConsume := []string{"sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ChangeMessageVisibility"}
		statements := []any{map[string]any{"Effect": "Allow", "Action": []string{"logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"}, "Resource": "arn:aws:logs:*:*:*"}}
		switch name {
		case "scheduler":
			statements = append(statements,
				map[string]any{"Effect": "Allow", "Action": []string{"dynamodb:Query", "dynamodb:UpdateItem"}, "Resource": tableResources},
				map[string]any{"Effect": "Allow", "Action": "sqs:SendMessage", "Resource": values[2].(string)},
			)
		case "feed-worker":
			statements = append(statements,
				map[string]any{"Effect": "Allow", "Action": []string{"dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem"}, "Resource": tableResources},
				map[string]any{"Effect": "Allow", "Action": "s3:PutObject", "Resource": values[1].(string) + "/favicons/*"},
				map[string]any{"Effect": "Allow", "Action": queueConsume, "Resource": values[2].(string)},
				map[string]any{"Effect": "Allow", "Action": "sqs:SendMessage", "Resource": values[3].(string)},
			)
		case "item-worker":
			statements = append(statements,
				map[string]any{"Effect": "Allow", "Action": []string{"dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Query", "dynamodb:TransactWriteItems", "dynamodb:UpdateItem"}, "Resource": tableResources},
				map[string]any{"Effect": "Allow", "Action": []string{"s3:PutObject", "s3:GetObject"}, "Resource": []string{values[1].(string) + "/bodies/*", values[1].(string) + "/media/*"}},
				map[string]any{"Effect": "Allow", "Action": queueConsume, "Resource": values[3].(string)},
				map[string]any{"Effect": "Allow", "Action": []string{"s3vectors:PutVectors", "s3vectors:QueryVectors", "s3vectors:GetVectors"}, "Resource": values[4].(string)},
			)
		case "api":
			statements = append(statements,
				map[string]any{"Effect": "Allow", "Action": []string{"dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:DeleteItem", "dynamodb:Query", "dynamodb:BatchGetItem", "dynamodb:BatchWriteItem", "dynamodb:TransactWriteItems"}, "Resource": tableResources},
				map[string]any{"Effect": "Allow", "Action": "s3:GetObject", "Resource": []string{values[1].(string) + "/bodies/*", values[1].(string) + "/media/*"}},
				map[string]any{"Effect": "Allow", "Action": []string{"s3:PutObject", "s3:DeleteObject"}, "Resource": values[1].(string) + "/archive/*"},
				map[string]any{"Effect": "Allow", "Action": "s3:PutObject", "Resource": values[1].(string) + "/favicons/*"},
				map[string]any{"Effect": "Allow", "Action": "sqs:SendMessage", "Resource": []string{values[2].(string), values[3].(string)}},
				map[string]any{"Effect": "Allow", "Action": []string{"s3vectors:PutVectors", "s3vectors:DeleteVectors", "s3vectors:QueryVectors", "s3vectors:GetVectors"}, "Resource": values[4].(string)},
			)
		case "rescore":
			statements = append(statements,
				map[string]any{"Effect": "Allow", "Action": []string{"dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:DeleteItem", "dynamodb:Query", "dynamodb:BatchGetItem", "dynamodb:BatchWriteItem"}, "Resource": tableResources},
			)
		case "vector-cleanup":
			statements = append(statements,
				map[string]any{"Effect": "Allow", "Action": []string{"s3vectors:ListVectors", "s3vectors:GetVectors", "s3vectors:DeleteVectors"}, "Resource": values[4].(string)},
			)
		}
		modelResources := make([]string, 0, len(bedrockModels))
		for _, model := range bedrockModels {
			if strings.TrimSpace(model) != "" {
				modelResources = append(modelResources, "arn:aws:bedrock:us-east-1::foundation-model/"+model)
			}
		}
		if len(modelResources) > 0 {
			statements = append(statements, map[string]any{"Effect": "Allow", "Action": "bedrock:InvokeModel", "Resource": modelResources})
		}
		encoded, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": statements})
		return string(encoded), err
	}).(pulumi.StringOutput)
	_, err = iam.NewRolePolicy(ctx, name+"-policy", &iam.RolePolicyArgs{Role: role.ID(), Policy: policy})
	return role, err
}

func function(ctx *pulumi.Context, name string, role *iam.Role, memory, timeout, concurrency int, environment pulumi.StringMap) (*awslambda.Function, error) {
	args := &awslambda.FunctionArgs{
		Name: pulumi.Sprintf("sema-%s-%s", ctx.Stack(), name), Role: role.Arn, Runtime: pulumi.String("provided.al2023"), Architectures: pulumi.StringArray{pulumi.String("arm64")}, Handler: pulumi.String("bootstrap"),
		Code: pulumi.NewFileArchive(filepath.Join("..", "bin", name+".zip")), MemorySize: pulumi.Int(memory), Timeout: pulumi.Int(timeout), Environment: &awslambda.FunctionEnvironmentArgs{Variables: environment},
	}
	if concurrency > 0 {
		args.ReservedConcurrentExecutions = pulumi.Int(concurrency)
	}
	fn, err := awslambda.NewFunction(ctx, name, args)
	if err != nil {
		return nil, err
	}
	_, err = cloudwatch.NewLogGroup(ctx, name+"-logs", &cloudwatch.LogGroupArgs{Name: pulumi.Sprintf("/aws/lambda/%s", fn.Name), RetentionInDays: pulumi.Int(14)})
	return fn, err
}

func staticBehavior(origin string) *cloudfront.DistributionDefaultCacheBehaviorArgs {
	return &cloudfront.DistributionDefaultCacheBehaviorArgs{TargetOriginId: pulumi.String(origin), ViewerProtocolPolicy: pulumi.String("redirect-to-https"), AllowedMethods: pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD"), pulumi.String("OPTIONS")}, CachedMethods: pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")}, Compress: pulumi.Bool(true), MinTtl: pulumi.Int(0), DefaultTtl: pulumi.Int(3600), MaxTtl: pulumi.Int(86400), ForwardedValues: &cloudfront.DistributionDefaultCacheBehaviorForwardedValuesArgs{QueryString: pulumi.Bool(false), Cookies: &cloudfront.DistributionDefaultCacheBehaviorForwardedValuesCookiesArgs{Forward: pulumi.String("none")}}}
}

func contentBehavior(pattern, origin string, trusted pulumi.StringInput) *cloudfront.DistributionOrderedCacheBehaviorArgs {
	behavior := &cloudfront.DistributionOrderedCacheBehaviorArgs{PathPattern: pulumi.String(pattern), TargetOriginId: pulumi.String(origin), ViewerProtocolPolicy: pulumi.String("redirect-to-https"), AllowedMethods: pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")}, CachedMethods: pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")}, Compress: pulumi.Bool(true), MinTtl: pulumi.Int(0), DefaultTtl: pulumi.Int(86400), MaxTtl: pulumi.Int(604800), ForwardedValues: &cloudfront.DistributionOrderedCacheBehaviorForwardedValuesArgs{QueryString: pulumi.Bool(false), Cookies: &cloudfront.DistributionOrderedCacheBehaviorForwardedValuesCookiesArgs{Forward: pulumi.String("none")}}}
	if trusted != nil {
		behavior.TrustedKeyGroups = pulumi.StringArray{trusted}
	}
	return behavior
}

func apiBehavior(pattern, origin string) *cloudfront.DistributionOrderedCacheBehaviorArgs {
	return &cloudfront.DistributionOrderedCacheBehaviorArgs{PathPattern: pulumi.String(pattern), TargetOriginId: pulumi.String(origin), ViewerProtocolPolicy: pulumi.String("https-only"), AllowedMethods: pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD"), pulumi.String("OPTIONS"), pulumi.String("PUT"), pulumi.String("POST"), pulumi.String("PATCH"), pulumi.String("DELETE")}, CachedMethods: pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")}, Compress: pulumi.Bool(true), MinTtl: pulumi.Int(0), DefaultTtl: pulumi.Int(0), MaxTtl: pulumi.Int(0), ForwardedValues: &cloudfront.DistributionOrderedCacheBehaviorForwardedValuesArgs{QueryString: pulumi.Bool(true), Headers: pulumi.StringArray{pulumi.String("Content-Type"), pulumi.String("Sec-Fetch-Site")}, Cookies: &cloudfront.DistributionOrderedCacheBehaviorForwardedValuesCookiesArgs{Forward: pulumi.String("all")}}}
}

func merge(maps ...pulumi.StringMap) pulumi.StringMap {
	result := pulumi.StringMap{}
	for _, values := range maps {
		for key, value := range values {
			result[key] = value
		}
	}
	return result
}

func uploadWeb(ctx *pulumi.Context, bucket *s3.Bucket) error {
	directory := filepath.Join("..", "web", "dist")
	if _, err := os.Stat(directory); err != nil {
		return fmt.Errorf("frontend build output is unavailable: %w", err)
	}
	return filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		relative, _ := filepath.Rel(directory, path)
		name := "app-" + strings.NewReplacer("/", "-", ".", "-").Replace(relative)
		contentType := mime.TypeByExtension(filepath.Ext(path))
		cacheControl := webCacheControl(relative)
		_, err = s3.NewBucketObject(ctx, name, &s3.BucketObjectArgs{Bucket: bucket.ID(), Key: pulumi.String(filepath.ToSlash(relative)), Source: pulumi.NewFileAsset(path), ContentType: pulumi.String(contentType), CacheControl: pulumi.String(cacheControl)})
		return err
	})
}

func webCacheControl(relative string) string {
	switch filepath.ToSlash(relative) {
	case "index.html", "sw.js":
		return "no-cache"
	case "manifest.webmanifest":
		return "public,max-age=300,must-revalidate"
	case "icon-192.png", "icon-512.png", "apple-touch-icon.png", "sema-mark.svg", "sema-mark-small.svg", "favicon.ico", "favicon.svg", "favicon-32.png", "favicon-16.png":
		return "public,max-age=86400"
	default:
		return "public,max-age=31536000,immutable"
	}
}

func buildDeployAssets(googleClientID string) error {
	repository, err := filepath.Abs("..")
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	command := exec.Command("make", "build")
	command.Dir = repository
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "VITE_GOOGLE_CLIENT_ID=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "VITE_GOOGLE_CLIENT_ID="+googleClientID)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build deployment assets: %w", err)
	}
	return nil
}
