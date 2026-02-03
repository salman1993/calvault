-- Meetings by organizer in the last 3 months
SELECT organizer_email, COUNT(*) as meetings
FROM events
WHERE start_time > date('now', '-3 months')
GROUP BY organizer_email
ORDER BY meetings DESC
LIMIT 10;
