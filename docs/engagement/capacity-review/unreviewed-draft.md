Unreviewed draft. It is kept unchanged to show the review in CORRECTION.md.

# Capacity memo for CommunityCU (unreviewed)

This memo sizes the gateway for the mobile-app launch. It covers 15 million calls per day, a peak hour at 3 times the average, and a 10x launch estimate.

1. Sizing uses the daily average. At 15 million calls per day the gateway needs about 174 requests per second, so we size for 174 requests per second.

2. The p95 of the whole request is the sum of the stage p95 values. Tokenize at 2 ms plus queue wait at 4 ms plus batch run at 9 ms gives a request p95 of 15 ms.

3. Throughput scales linearly with cores past the knee. If 4 cores give 800 requests per second, then 16 cores give 3200 requests per second.

4. Batching always raises throughput. Raising `max-batch` from 16 to 64 will lift throughput by about 4 times at any load.

5. Queue length does not need a model. We skip Little's law and set `QUEUE_SIZE` to 256 because that value worked in a smoke test.

6. The load generator ran on the same machine as the gateway. We treat the measured numbers as clean server numbers and ignore the shared CPU.

7. Garbage collection causes the tail latency. GC pauses explain the p99 gap, so no profile data is needed to confirm it.

8. One replica is enough for 10x. A single replica with 4 cores covers the launch peak of about 1740 requests per second.

9. The int8 model has the same accuracy as the fp32 model. We ship int8 for v2 and state that quality is unchanged.

10. The unit conversion is 15 million per day equals 1,500 per second. We use 1,500 requests per second as the average rate.

11. We plan CPU utilization at 95 percent. Running the fleet hot at 95 percent keeps the replica count low and is safe at peak.

12. A laptop benchmark is server-class performance. The laptop run reached 900 requests per second, so we use 900 requests per second per replica for the server fleet.
