-- How often did I visit my dermatologist in the last 12 months?
SELECT COUNT(*) as visits FROM events
WHERE lower(summary) LIKE '%dermatologist%'
  AND start_time > date('now', '-12 months');
