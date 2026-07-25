.PHONY: test dev simulate simulate-tigris doctor

test:
	mise run test

dev:
	mise run dev

simulate:
	mise run simulate

simulate-tigris:
	mise run simulate-tigris

doctor:
	mise run doctor
