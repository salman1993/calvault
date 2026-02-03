-- Busiest dates by number of events
SELECT
  date(start_time) as date,
  COUNT(*) as events
FROM events
WHERE start_time IS NOT NULL
GROUP BY date(start_time)
ORDER BY events DESC
LIMIT 20;
