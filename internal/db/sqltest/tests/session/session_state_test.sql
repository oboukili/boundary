begin;
    select plan(9);
    select wtt_load('widgets', 'iam', 'kms', 'auth', 'hosts', 'targets', 'credentials','sessions');

    -- Ensure session state table is populated
    select is(count(*), 1::bigint) from session_state where session_id = 's1____warren';
    select is(count(*), 1::bigint) from session_state where session_id = 's1____warren' and state='pending';
    select is(count(*), 1::bigint) from session_state where session_id = 's1____warren' and prior_state='pending';

    -- Disabling trigger; insert_session_state() uses now() and causes pkey violation otherwise
    ALTER TABLE session_state DISABLE TRIGGER insert_session_state;

    -- Valid state transition
    insert into session_state
    ( session_id, state, start_time)
    values
        ('s1____warren','active', clock_timestamp()+ INTERVAL '1 minute');

    select is(count(*), 2::bigint) from session_state where session_id = 's1____warren';
    select is(count(*), 1::bigint) from session_state where session_id = 's1____warren' and state='active';

    -- Invalid duplicate state
    select throws_ok($$ insert into session_state  ( session_id, state, start_time )
    values  ('s1____warren','active', clock_timestamp()+ INTERVAL '2 minute')$$);

    -- Valid state transition
    insert into session_state
    ( session_id, state, start_time)
    values
        ('s1____warren','terminated', clock_timestamp()+ INTERVAL '2 minute');
    select is(count(*), 3::bigint) from session_state where session_id = 's1____warren';
    select is(count(*), 1::bigint) from session_state where session_id = 's1____warren' and state='terminated';

    -- Invalid state transition
    select throws_ok($$ insert into session_state  ( session_id, state, start_time )
    values  ('s1____warren','pending', clock_timestamp()+ INTERVAL '3 minute')$$);

    ALTER TABLE session_state ENABLE TRIGGER insert_session_state;

rollback;