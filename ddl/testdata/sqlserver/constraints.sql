-- actor

-- address

-- category

-- city

-- country

-- customer
ALTER TABLE customer ADD CONSTRAINT customer_email_first_name_last_name_key UNIQUE (email, first_name, last_name);
ALTER TABLE customer ADD CONSTRAINT customer_email_key UNIQUE (email);

-- department

-- employee

-- employee_department

-- film
ALTER TABLE film ADD CONSTRAINT film_rating_check CHECK ([rating]='NC-17' OR [rating]='R' OR [rating]='PG-13' OR [rating]='PG' OR [rating]='G');
ALTER TABLE film ADD CONSTRAINT film_year_check CHECK ([release_year]>=(1901) AND [release_year]<=(2155));

-- film_actor

-- film_category

-- inventory

-- language

-- payment

-- rental

-- staff
ALTER TABLE staff ADD CONSTRAINT staff_email_key UNIQUE (email);

-- store

-- task

-- actor

-- address
ALTER TABLE address ADD CONSTRAINT address_city_id_fkey FOREIGN KEY (city_id) REFERENCES city (city_id) ON UPDATE CASCADE;

-- category

-- city
ALTER TABLE city ADD CONSTRAINT city_country_id_fkey FOREIGN KEY (country_id) REFERENCES country (country_id) ON UPDATE CASCADE;

-- country

-- customer
ALTER TABLE customer ADD CONSTRAINT customer_address_id_fkey FOREIGN KEY (address_id) REFERENCES address (address_id) ON UPDATE CASCADE;
ALTER TABLE customer ADD CONSTRAINT customer_store_id_fkey FOREIGN KEY (store_id) REFERENCES store (store_id) ON UPDATE CASCADE;

-- department

-- employee
ALTER TABLE employee ADD CONSTRAINT employee_manager_id_fkey FOREIGN KEY (manager_id) REFERENCES employee (employee_id);

-- employee_department
ALTER TABLE employee_department ADD CONSTRAINT employee_department_department_id_fkey FOREIGN KEY (department_id) REFERENCES department (department_id);
ALTER TABLE employee_department ADD CONSTRAINT employee_department_employee_id_fkey FOREIGN KEY (employee_id) REFERENCES employee (employee_id);

-- film
ALTER TABLE film ADD CONSTRAINT film_language_id_fkey FOREIGN KEY (language_id) REFERENCES language (language_id);
ALTER TABLE film ADD CONSTRAINT film_original_language_id_fkey FOREIGN KEY (original_language_id) REFERENCES language (language_id);

-- film_actor
ALTER TABLE film_actor ADD CONSTRAINT film_actor_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES actor (actor_id) ON UPDATE CASCADE;
ALTER TABLE film_actor ADD CONSTRAINT film_actor_film_id_fkey FOREIGN KEY (film_id) REFERENCES film (film_id) ON UPDATE CASCADE;

-- film_category
ALTER TABLE film_category ADD CONSTRAINT film_category_category_id_fkey FOREIGN KEY (category_id) REFERENCES category (category_id) ON UPDATE CASCADE;
ALTER TABLE film_category ADD CONSTRAINT film_category_film_id_fkey FOREIGN KEY (film_id) REFERENCES film (film_id) ON UPDATE CASCADE;

-- inventory
ALTER TABLE inventory ADD CONSTRAINT inventory_film_id_fkey FOREIGN KEY (film_id) REFERENCES film (film_id) ON UPDATE CASCADE;
ALTER TABLE inventory ADD CONSTRAINT inventory_store_id_fkey FOREIGN KEY (store_id) REFERENCES store (store_id) ON UPDATE CASCADE;

-- language

-- payment
ALTER TABLE payment ADD CONSTRAINT payment_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES customer (customer_id);
ALTER TABLE payment ADD CONSTRAINT payment_rental_id_fkey FOREIGN KEY (rental_id) REFERENCES rental (rental_id) ON UPDATE CASCADE ON DELETE SET NULL;
ALTER TABLE payment ADD CONSTRAINT payment_staff_id_fkey FOREIGN KEY (staff_id) REFERENCES staff (staff_id);

-- rental
ALTER TABLE rental ADD CONSTRAINT rental_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES customer (customer_id);
ALTER TABLE rental ADD CONSTRAINT rental_inventory_id_fkey FOREIGN KEY (inventory_id) REFERENCES inventory (inventory_id);
ALTER TABLE rental ADD CONSTRAINT rental_staff_id_fkey FOREIGN KEY (staff_id) REFERENCES staff (staff_id);

-- staff
ALTER TABLE staff ADD CONSTRAINT staff_address_id_fkey FOREIGN KEY (address_id) REFERENCES address (address_id) ON UPDATE CASCADE;

-- store
ALTER TABLE store ADD CONSTRAINT store_address_id_fkey FOREIGN KEY (address_id) REFERENCES address (address_id);
ALTER TABLE store ADD CONSTRAINT store_manager_staff_id_fkey FOREIGN KEY (manager_staff_id) REFERENCES staff (staff_id);

-- task
ALTER TABLE task ADD CONSTRAINT task_employee_id_department_id_fkey FOREIGN KEY (employee_id, department_id) REFERENCES employee_department (employee_id, department_id);
