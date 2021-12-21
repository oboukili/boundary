begin;

-- Remove invalid (states with no prior end time) rows
delete from session_state where previous_end_time is null and state != 'pending';

-- In the case of dupes that are otherwise valid, identify first valid session
create temp table session_state_delete
as select session_id, min(start_time) as st_t from session_state ou
   where (select count(*) from session_state inr
          where inr.session_id = ou.session_id and inr.state = ou.state) > 1
   group by session_id;

-- Remove all session rows after first valid session
DELETE FROM session_state t1
    using session_state_delete t2 where t1.session_id =t2.session_id and t1.start_time > t2.st_t;

drop table session_state_delete;

-- Update session_state so any active/ pending sessions are set to canceling
-- This will cancel sessions when we close all open session connections
insert into session_state (session_id, state)
    (select s1.session_id, 'canceling' from session_state s1
        inner join (
            select session_id, max(start_time) as st from session_state
            group by session_id) s2
        on s1.session_id=s2.session_id and s1.start_time=s2.st
     where state in('pending','active'));

-- Close all open session_connections
-- This will trigger the closure of sessions: update_connection_state_on_closed_reason -> terminate_session_if_possible
update session_connection
set closed_reason='canceled' where public_id in
    (select public_id from session_connection where closed_reason is null);

-- Migration

-- session_valid_state table creation and related constraints
create table session_valid_state(
    prior_state text
        references session_state_enm(name)
            on delete restrict
            on update cascade,
        constraint valid_prior_states
        check (
                prior_state in ('pending', 'active', 'canceling')
            ),
    current_state text
        references session_state_enm(name)
            on delete restrict
            on update cascade,
        constraint valid_current_states
        check (
            current_state in ('pending', 'active', 'canceling', 'terminated')
        ),
    primary key (prior_state, current_state)
);

insert into session_valid_state (prior_state, current_state)
values
    ('pending','pending'),
    ('pending','active'),
    ('pending','terminated'),
    ('pending','canceling'),
    ('active','canceling'),
    ('active','terminated'),
    ('canceling','terminated');

alter table session_state
    add column prior_state text not null default 'pending'
        references session_state_enm(name)
        on delete restrict
        on update cascade;
alter table session_state
    add foreign key (prior_state, state)
      references session_valid_state (prior_state,current_state);
alter table session_state
    add unique (session_id, state);

create or replace function
    update_prior_session_state()
    returns trigger
as $$
begin
    -- Prior state is the most recent valid prior state entry for this session_id
    new.prior_state = query.state from(
      select state from session_state where session_id=new.session_id and state in(
          select prior_state from session_valid_state where current_state=new.state )
      order by start_time desc limit 1) as query;

    if new.prior_state is null then
        new.prior_state='pending';
    end if;

    return new;

end;
$$ language plpgsql;

create trigger update_session_state before insert on session_state
    for each row execute procedure update_prior_session_state();

commit;