# Send is a public, versioned webhook contract

Send posts one Item to the user's Destination as a signed webhook. Any receiver can implement it, and Sema says nothing about what the Destination does with the Item. Because receivers outside Sema depend on the payload, it is a public contract: within `version: 1` we only add optional fields. Code and docs never name a specific Destination, so the contract never takes on one receiver's shape.

Payload fields use the `CONTEXT.md` vocabulary (`kept`, `feed`, feed `tags`).

## Images

`images` lists the stored origin lead URL first, then a one-hour CloudFront-signed URL to a per-Send copy of Sema's largest stored image under `send/`. Some publisher pages block receivers, and the copy means a receiver never depends on them. Each Send makes its own copy instead of signing the stored image, because stored keys embed the user's Google subject. A signed URL to a stored key would hand that subject to the receiver.

## Delivery

The first attempt runs inline in the API request, so the user sees the real status rather than "queued". Retryable failures go to the deliveries queue. Each retry is re-signed with the current secret, so rotating the secret also covers retries already in flight.

## Storage

The Destination and its secret live in a separate `DESTINATION` row, never in the profile, so reads of the profile never carry the secret. The secret is write-only over the API.
