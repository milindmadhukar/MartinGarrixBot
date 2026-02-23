This is a Martin Garrix / STMPD RCRDS Community based puropse-built discord bot written in Golang with the help of Disgo package.
Imporant commands handling migrations, sqlc generation are in the Makefile

Listeners will always go in the listeners/ folder
Commands always go in the commands/ folder

Any utility / resusable code that is not part of the core logic should go in the utils folder

We will use a cache first then rest type approach, always check the Caches() and then only go for Rest() calls on cache misses unless it is always better to do Rest calls when we know it
will almost most certainly be a cache miss most times then