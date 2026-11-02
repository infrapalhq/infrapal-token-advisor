-- 0018_drop_finding_state: the Fixes page keeps its history in fix_marks (0014),
-- so nothing reads or writes this table any more.
DROP TABLE finding_state;
