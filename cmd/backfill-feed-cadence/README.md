# Feed Cadence migration

Deploy the API, feed worker, and item worker changes before running this command. New feeds start on Auto; existing feeds remain on their stored Cadence until migration. Publish history can accumulate before migration, but Auto's learning week starts at the recorded migration time.

Preview the changes (no writes):

```sh
make backfill-feed-cadence STACK=prod
```

Apply the reviewed plan:

```sh
make backfill-feed-cadence STACK=prod BACKFILL_ARGS=--apply
```

Hourly non-Reddit feeds become Auto. Three-, six-, and twenty-four-hour pins and Reddit collection Cadence remain intact. Each feed missing `history_started_at` receives the same migration timestamp. Feeds with that timestamp are skipped, including feeds pinned hourly after migration, so rerunning is safe.

The API keeps `fetch_interval_h` numeric for existing clients. `cadence_pin_h` is null for Auto, and `effective_cadence_h` reports the current Cadence. PATCH `fetch_interval_h: null` clears a pin; omitting the field preserves it when editing other settings.

After rollout, check feed-worker logs for `feed refused` with `connector`, `host`, `status`, and `refusals`. Refusals should not increment `FeedsFailed`; continuous refusals become broken only after twenty-four hours. Item logs expose `LinkItems` separately from `ExtractionFailed`. Neither new count adds an extracted CloudWatch metric.
