package _

import "github.com/bokwoon95/sq"

type ACTOR struct {
	sq.TableStruct
	ACTOR_ID sq.NumberField `ddl:"primarykey identity"`
	NAME     sq.StringField
}

type MOVIE struct {
	sq.TableStruct `sq:"bar.movie"`
	MOVIE_ID       sq.NumberField `ddl:"primarykey identity"`
	TITLE          sq.StringField `ddl:"index"`
	SYNOPSIS       sq.StringField
}

type MOVIE_ACTOR struct {
	sq.TableStruct `sq:"bar.movie_actor"`
	MOVIE_ID       sq.NumberField `ddl:"references=bar.movie.movie_id"`
	ACTOR_ID       sq.NumberField `ddl:"references=actor.actor_id"`
}

type MOVIE_AWARD struct {
	sq.TableStruct          `sq:"bar.movie_award"`
	MOVIE_ID                sq.NumberField `ddl:"references=bar.movie.movie_id"`
	BEST_ACTOR              sq.NumberField `ddl:"references=actor.actor_id"`
	BEST_SUPPORTING_ACTOR   sq.NumberField `ddl:"references=actor.actor_id"`
	BEST_ACTRESS            sq.NumberField `ddl:"references=actor.actor_id"`
	BEST_SUPPORTING_ACTRESS sq.NumberField `ddl:"references=actor.actor_id"`
}
