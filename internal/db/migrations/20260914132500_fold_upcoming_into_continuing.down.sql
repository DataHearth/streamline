-- reverse: fold the removed "upcoming" series_status into "continuing"
--
-- Deliberately a no-op. The up migration is lossy — once an upcoming show
-- reads "continuing" there is nothing left to tell it from a show that was
-- always continuing, and guessing from air dates would relabel rows this
-- migration never touched. A downgrade that needs the value back gets it from
-- the next metadata refresh, not from here.
SELECT 1;
