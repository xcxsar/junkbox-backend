DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT tablename FROM pg_tables
    WHERE schemaname = 'public'
      -- artists y albums se excluyen: TRUNCATE ... CASCADE sobre ellas vaciaría
      -- también song_metadata a través de sus claves foráneas.
      AND tablename NOT IN ('goose_db_version', 'song_metadata', 'artists', 'albums')
    ORDER BY tablename
  LOOP
    EXECUTE format('TRUNCATE TABLE %I RESTART IDENTITY CASCADE', r.tablename);
  END LOOP;
END
$$;
