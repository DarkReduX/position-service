CREATE FUNCTION notify_trigger() RETURNS trigger as $trigger$
DECLARE
    rec RECORD;
BEGIN

    CASE TG_OP
        WHEN 'INSERT' THEN
            rec := NEW;
        ELSE
            RAISE EXCEPTION 'Unknown TG_OP: "%". Should not occur!', TG_OP;
    END CASE;

    perform pg_notify('db_notifications', row_to_json(NEW)::text);

    return rec;
end;


$trigger$ LANGUAGE plpgsql
