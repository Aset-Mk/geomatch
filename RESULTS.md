## Окружение

- **CPU:** Intel Core i7-12700H, 20 потоков
- **RAM:** 16 GB
- **ОС:** Linux 7.2 (Arch)
- **Go:** 1.27.3
- **Режим процессора:** performance, ноутбук от сети

## Как повторить

```
$ echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor
$ make build
$ ORDER_RATE=10 ./bin/loadgen
$ ORDER_RATE=20 ./bin/loadgen
$ ORDER_RATE=30 ./bin/loadgen
$ ORDER_RATE=40 ./bin/loadgen
$ ORDER_RATE=50 ./bin/loadgen
$ ORDER_RATE=60 ./bin/loadgen
$ ORDER_RATE=61 ./bin/loadgen
$ ORDER_RATE=62 ./bin/loadgen
$ ORDER_RATE=63 ./bin/loadgen
$ ORDER_RATE=64 ./bin/loadgen
$ ORDER_RATE=65 ./bin/loadgen
$ ORDER_RATE=66 ./bin/loadgen
$ ORDER_RATE=67 ./bin/loadgen
$ ORDER_RATE=68 ./bin/loadgen
$ ORDER_RATE=69 ./bin/loadgen
$ ORDER_RATE=70 ./bin/loadgen
$ ORDER_RATE=1000 ./bin/loadgen


$ ORDER_RATE=50 CPU_PROFILE=cpu.prof ./bin/loadgen
$ go tool pprof -top -nodecount=15 bin/loadgen cpu.prof
$ go tool pprof -http=:8080 bin/loadgen cpu.prof

$ ORDER_RATE=63 CPU_PROFILE=cpu63.prof ./bin/loadgen
$ go tool pprof -top -nodecount=15 bin/loadgen cpu63.prof
$ go tool pprof -http=:8080 bin/loadgen cpu63.prof

$ echo powersave | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor
```

## 2026-10-23 Наивный индекс

Map под одним мьютексом. `Nearest` перебирает всех водителей и сортирует по расстоянию. Коммит `cb3c785`.

ORDER_RATE меняется от прогона к прогону. Параметры по умолчанию:

```
DRIVERS=100000      водителей
UPDATE_PERIOD=2s    каждый водитель обновляет координаты раз в 2 с
DURATION=30s        длительность прогона
SEED=1              сценарий случайных чисел
OUT=results.json    файл с итогом
```

Задержка при разной частоте заказов. Значение таблицы - медиана по 5 прогонам на частоту. У 1000 все процентили упёрлись в верхнюю границу гистограммы:

| Заказов/с |   p50 (ms) |   p99 (ms) | p99.9 (ms) |   max (ms) | Прогон шёл (s) |
|-----------|------------|------------|------------|------------|----------------|
|        10 |       16.9 |       23.3 |       25.5 |       25.5 |           30.0 |
|        20 |       17.0 |       23.9 |       25.3 |       26.0 |           30.0 |
|        30 |       16.9 |       23.6 |       25.4 |       25.7 |           30.0 |
|        40 |       16.9 |       23.3 |       25.6 |       25.8 |           30.0 |
|        50 |       16.8 |       23.1 |       25.8 |       25.9 |           30.0 |
|        60 |       17.0 |       25.4 |       27.2 |       28.0 |           30.0 |
|        61 |       17.5 |       25.4 |       26.2 |       26.9 |           30.0 |
|        62 |       44.1 |       79.7 |       81.7 |       81.8 |           30.0 |
|        63 |      276.7 |      466.9 |      469.8 |      469.8 |           30.5 |
|        64 |      541.2 |    1 065.0 |    1 070.1 |    1 071.1 |           31.1 |
|        65 |      788.5 |    1 547.3 |    1 558.5 |    1 559.6 |           31.6 |
|        66 |    1 041.9 |    2 001.9 |    2 016.3 |    2 017.3 |           32.0 |
|        67 |    1 272.8 |    2 467.8 |    2 488.3 |    2 490.4 |           32.5 |
|        68 |    1 450.0 |    2 906.1 |    2 928.6 |    2 932.7 |           32.9 |
|        69 |    1 766.4 |    3 444.7 |    3 473.4 |    3 477.5 |           33.5 |
|        70 |    1 976.3 |    3 813.4 |    3 844.1 |    3 848.2 |           33.8 |
|      1000 | >   60 000 | >   60 000 | >   60 000 | >   60 000 |          482.5 |

- **Предел около 61 заказа/с.** Наивный подход держит около 61 заказа в секунду. Я хочу добиться чтобы около 1000 в секунду обрабатывал, поэтому он слабее в 16 раз чем нужно.


Профиль процессора при 50 заказах/с

| flat (s) | flat % | sum % | cum (s) | cum % | Функция                                 |
|----------|--------|-------|---------|-------|-----------------------------------------|
|     6.68 |  24.92 | 24.92 |    7.10 | 26.48 | `cmp.Compare[float64]`                  |
|     6.39 |  23.83 | 48.75 |   13.38 | 49.91 | `slices.partitionCmpFunc[Neighbor]`     |
|     2.39 |   8.91 | 57.67 |    2.41 |  8.99 | `math.sin`                              |
|     1.94 |   7.24 | 64.90 |    1.94 |  7.24 | `math.xatan`                            |
|     1.46 |   5.45 | 70.35 |    3.65 | 13.61 | `math.asin`                             |
|     1.29 |   4.81 | 75.16 |    8.55 | 31.89 | `geo.Distance`                          |
|     1.05 |   3.92 | 79.07 |    1.85 |  6.90 | `slices.insertionSortCmpFunc[Neighbor]` |
|     1.04 |   3.88 | 82.95 |    1.10 |  4.10 | `math.cos`                              |
|     0.96 |   3.58 | 86.53 |    8.06 | 30.06 | `index.(*Naive).Nearest.func1`          |
|     0.51 |   1.90 | 88.44 |    0.51 |  1.90 | `runtime.futex`                         |
|     0.42 |   1.57 | 90.00 |    0.42 |  1.57 | `cmp.isNaN[float64]`                    |
|     0.23 |   0.86 | 90.86 |    0.31 |  1.16 | `maps.(*Iter).Next`                     |
|     0.23 |   0.86 | 91.72 |   16.04 | 59.83 | `slices.pdqsortCmpFunc[Neighbor]`       |
|     0.21 |   0.78 | 92.50 |    0.46 |  1.72 | `slices.order2CmpFunc[Neighbor]`        |
|     0.19 |   0.71 | 93.21 |    2.13 |  7.94 | `math.satan`                            |

![Flame graph наивного индекса при 50 заказах/с](docs/img/naive-flame-50.png)

Профиль при 63 заказах/с, выше предела. Доли почти те же, что при 50/с

| flat (s) | flat % | sum % | cum (s) | cum % | Функция                                 |
|----------|--------|-------|---------|-------|-----------------------------------------|
|     8.30 |  25.83 | 25.83 |    8.83 | 27.48 | `cmp.Compare[float64]`                  |
|     7.64 |  23.78 | 49.61 |   16.05 | 49.95 | `slices.partitionCmpFunc[Neighbor]`     |
|     2.97 |   9.24 | 58.85 |    2.97 |  9.24 | `math.sin`                              |
|     2.11 |   6.57 | 65.42 |    2.11 |  6.57 | `math.xatan`                            |
|     1.94 |   6.04 | 71.46 |    4.28 | 13.32 | `math.asin`                             |
|     1.56 |   4.86 | 76.31 |    2.71 |  8.43 | `slices.insertionSortCmpFunc[Neighbor]` |
|     1.55 |   4.82 | 81.14 |    9.92 | 30.87 | `geo.Distance`                          |
|     1.15 |   3.58 | 84.72 |    9.98 | 31.06 | `index.(*Naive).Nearest.func1`          |
|     0.91 |   2.83 | 87.55 |    0.96 |  2.99 | `math.cos`                              |
|     0.53 |   1.65 | 89.20 |    0.53 |  1.65 | `cmp.isNaN[float64]`                    |
|     0.50 |   1.56 | 90.76 |    0.50 |  1.56 | `runtime.futex`                         |
|     0.38 |   1.18 | 91.94 |    0.45 |  1.40 | `maps.(*Iter).Next`                     |
|     0.38 |   1.18 | 93.12 |   19.91 | 61.97 | `slices.pdqsortCmpFunc[Neighbor]`       |
|     0.19 |   0.59 | 93.71 |    0.59 |  1.84 | `slices.order2CmpFunc[Neighbor]`        |
|     0.18 |   0.56 | 94.27 |    2.29 |  7.13 | `math.satan`                            |

![Flame graph наивного индекса при 63 заказах/с](docs/img/naive-flame-63.png)

- **Причины задержки.** Почти всё время уходит на `index.(*Naive).Nearest`: около 60% на сортировку всех водителей (`slices.SortFunc`: 16.04 s при 50/с, 19.91 s при 63/с) и около 31% на расчёт расстояний (`geo.Distance`).
